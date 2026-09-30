// Guide 12: tenants connect their own Stripe account via OAuth. See README.md.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"

	"ella.to/stripe"
)

const addr = "localhost:4242"

// tenant is what you persist per connected account.
type tenant struct {
	AccountID   string
	AccessToken string  // acts as an API key for the tenant's account
	FeePercent  float64 // the tenant's own fee setting
}

// store stands in for your database. It also implements stripe.FeeResolver,
// so charges look up the tenant's fee from here instead of Stripe metadata.
type store struct {
	mu      sync.Mutex
	tenants map[string]*tenant
}

func (s *store) get(id string) (*tenant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tenants[id]
	return t, ok
}

func (s *store) save(t *tenant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants[t.AccountID] = t
}

func (s *store) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tenants, id)
}

func (s *store) ResolveFee(_ context.Context, accountID string) (stripe.PlatformFee, bool, error) {
	t, ok := s.get(accountID)
	if !ok {
		return stripe.PlatformFee{}, false, nil
	}
	return stripe.PlatformFee{Percent: t.FeePercent}, true, nil
}

func main() {
	db := &store{tenants: map[string]*tenant{}}

	// The platform client: only used for the OAuth handshake and disconnects.
	platform := stripe.New(
		mustEnv("STRIPE_SECRET_KEY"),
		stripe.WithWebhookSecret(mustEnv("STRIPE_WEBHOOK_SECRET")),
		stripe.WithOAuthClientID(mustEnv("STRIPE_CONNECT_CLIENT_ID")),
		stripe.WithOAuthRedirectURI("http://"+addr+"/callback"),
		stripe.WithFeeResolver(db),
	)

	hooks := platform.Webhooks(stripe.WithIgnoreAPIVersionMismatch()) // local testing: accept your account's API version
	// The tenant disconnected your platform from their Stripe dashboard.
	stripe.On(hooks, stripe.EventAccountApplicationDeauthorized, func(ctx context.Context, ev stripe.Event, _ *struct{}) error {
		db.remove(ev.Account)
		slog.Info("tenant disconnected", "account", ev.Account)
		return nil
	})

	mux := http.NewServeMux()
	mux.Handle("POST /webhook", hooks)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<h1>Tenants</h1><p><a href="/connect">Connect your Stripe account</a></p><ul>`)
		db.mu.Lock()
		for id, t := range db.tenants {
			fmt.Fprintf(w, `<li><a href="/tenant/%s">%s</a> — fee %.1f%%</li>`, id, id, t.FeePercent)
		}
		db.mu.Unlock()
		fmt.Fprint(w, "</ul>")
	})

	// 1. Send the tenant to Stripe with an unguessable state value.
	mux.HandleFunc("GET /connect", func(w http.ResponseWriter, r *http.Request) {
		state := randomString()
		http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: state, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		url, err := platform.ConnectAuthorizeURL(state)
		if err != nil {
			fail(w, "authorize url", err)
			return
		}
		http.Redirect(w, r, url, http.StatusFound)
	})

	// 2. Stripe redirects back here. Check the state, then exchange the code.
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			http.Error(w, "connect cancelled: "+q.Get("error_description"), http.StatusBadRequest)
			return
		}
		cookie, err := r.Cookie("oauth_state")
		if err != nil || cookie.Value == "" || cookie.Value != q.Get("state") {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "oauth_state", Path: "/", MaxAge: -1})

		tenantClient, tok, err := platform.ClientFromOAuthCode(r.Context(), q.Get("code"))
		if err != nil {
			fail(w, "oauth exchange", err)
			return
		}
		db.save(&tenant{AccountID: tenantClient.AccountID(), AccessToken: tok.AccessToken, FeePercent: 5})
		slog.Info("tenant connected", "account", tenantClient.AccountID())
		http.Redirect(w, r, "/tenant/"+tenantClient.AccountID(), http.StatusSeeOther)
	})

	// 3. Tenant page: change their own fee, and act as the tenant.
	mux.HandleFunc("GET /tenant/{id}", func(w http.ResponseWriter, r *http.Request) {
		t, ok := db.get(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `<h1>Tenant %[1]s</h1>
<form method="POST" action="/tenant/%[1]s/fee">Fee %%: <input name="percent" value="%.1[2]f"> <button>Save</button></form>
<form method="POST" action="/tenant/%[1]s/customer"><button>Create a customer on this account</button></form>
<form method="POST" action="/tenant/%[1]s/disconnect"><button>Disconnect</button></form>
<p><a href="/">All tenants</a></p>`, t.AccountID, t.FeePercent)
	})

	// A fee change is just a write to your store; the FeeResolver serves it.
	mux.HandleFunc("POST /tenant/{id}/fee", func(w http.ResponseWriter, r *http.Request) {
		t, ok := db.get(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		pct, err := strconv.ParseFloat(r.FormValue("percent"), 64)
		if err != nil || pct < 0 || pct > 100 {
			http.Error(w, "percent must be 0-100", http.StatusBadRequest)
			return
		}
		t.FeePercent = pct
		db.save(t)
		fee, _, _ := platform.GetPlatformFee(r.Context(), t.AccountID)
		slog.Info("fee updated", "account", t.AccountID, "percent", fee.Percent)
		http.Redirect(w, r, "/tenant/"+t.AccountID, http.StatusSeeOther)
	})

	// Rebuild a client from the stored token: every call runs as the tenant.
	mux.HandleFunc("POST /tenant/{id}/customer", func(w http.ResponseWriter, r *http.Request) {
		t, ok := db.get(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		tenantClient := stripe.New(t.AccessToken, stripe.WithFeeResolver(db))
		cus, err := tenantClient.CreateCustomer(r.Context(), stripe.CreateCustomerParams{
			Email: "customer-of-tenant@example.com",
		})
		if err != nil {
			fail(w, "create customer as tenant", err)
			return
		}
		slog.Info("customer created on tenant account", "account", t.AccountID, "customer", cus.ID)
		fmt.Fprintf(w, `<p>Created %s on %s (check their dashboard).</p><a href="/tenant/%s">Back</a>`, cus.ID, t.AccountID, t.AccountID)
	})

	// 4. Disconnect from your side.
	mux.HandleFunc("POST /tenant/{id}/disconnect", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := platform.DisconnectAccount(r.Context(), id); err != nil {
			fail(w, "disconnect", err)
			return
		}
		db.remove(id)
		slog.Info("tenant disconnected", "account", id)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	slog.Info("listening", "url", "http://"+addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fatal("server stopped", "err", err)
	}
}

func randomString() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func fail(w http.ResponseWriter, msg string, err error) {
	slog.Error(msg, "err", err)
	http.Error(w, msg+": "+err.Error(), http.StatusBadGateway)
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		fatal("missing environment variable", "name", name)
	}
	return v
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
