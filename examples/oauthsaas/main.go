// Command oauthsaas shows a multi-tenant SaaS where each tenant connects their
// own Stripe account via Connect OAuth (instead of pasting a secret key), and
// then manages their own platform fee.
//
// The meta-platform keeps a single secret key, used only for the OAuth
// handshake. Day-to-day, every tenant is driven by the access token returned
// from the OAuth exchange, so the SDK operates as that tenant's account.
//
// Each tenant's chosen platform-fee rate lives in the SaaS's own store and is
// surfaced to the SDK through a FeeResolver - so a tenant adjusting their fee is
// just a write to your database.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"ella.to/stripe"
)

// tenant is what we persist per connected SaaS customer.
type tenant struct {
	AccountID   string
	AccessToken string
	FeePercent  float64
	FeeFixed    int64
}

// store is a stand-in for your database. It also implements stripe.FeeResolver
// so the SDK can ask "what fee does this account charge?".
type store struct {
	mu      sync.RWMutex
	tenants map[string]*tenant // keyed by Stripe account id (acct_...)
}

func newStore() *store { return &store{tenants: map[string]*tenant{}} }

func (s *store) save(t *tenant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants[t.AccountID] = t
}

func (s *store) get(accountID string) (*tenant, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[accountID]
	return t, ok
}

// ResolveFee implements stripe.FeeResolver, sourcing each tenant's fee from us.
func (s *store) ResolveFee(_ context.Context, accountID string) (stripe.PlatformFee, bool, error) {
	t, ok := s.get(accountID)
	if !ok {
		return stripe.PlatformFee{}, false, nil
	}
	return stripe.PlatformFee{Percent: t.FeePercent, Fixed: t.FeeFixed}, true, nil
}

func main() {
	db := newStore()

	// The meta-platform client (its own secret key + Connect OAuth app id). Used
	// only to build authorize URLs and to exchange OAuth codes.
	platform := stripe.New(
		os.Getenv("STRIPE_SECRET_KEY"),
		stripe.WithOAuthClientID(os.Getenv("STRIPE_CONNECT_CLIENT_ID")),
	)

	// 1. Start the connect flow: redirect the tenant to Stripe.
	http.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		url, err := platform.ConnectAuthorizeURL("state-from-session")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, url, http.StatusFound)
	})

	// 2. OAuth callback: exchange the code for the tenant's access token and
	//    build a client that operates AS the tenant.
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		// (verify r.URL.Query().Get("state") matches the session here)

		tenantClient, tok, err := platform.ClientFromOAuthCode(r.Context(), code)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		// Persist the connection. Default the tenant's platform fee to 5%.
		db.save(&tenant{
			AccountID:   tok.StripeUserID,
			AccessToken: tok.AccessToken,
			FeePercent:  5,
		})

		// tenantClient is now keyed by the access token; e.g. fetch their balance,
		// create products, charge their sub-merchants, etc. - all as the tenant.
		_ = tenantClient
		fmt.Fprintf(w, "connected account %s (token id %s)\n", tenantClient.AccountID(), tok.StripeUserID)
	})

	// 3. Tenant adjusts their OWN platform fee. This is just a write to our store;
	//    the FeeResolver picks it up on the next charge automatically.
	http.HandleFunc("/fee", func(w http.ResponseWriter, r *http.Request) {
		accountID := r.FormValue("account")
		t, ok := db.get(accountID)
		if !ok {
			http.Error(w, "unknown tenant", http.StatusNotFound)
			return
		}
		t.FeePercent, _ = strconv.ParseFloat(r.FormValue("percent"), 64)
		if fixed, err := strconv.ParseInt(r.FormValue("fixed"), 10, 64); err == nil {
			t.FeeFixed = fixed
		}
		db.save(t)
		fmt.Fprintf(w, "fee for %s set to %.2f%% + %d\n", accountID, t.FeePercent, t.FeeFixed)
	})

	// Example of building a tenant client later from a stored token and charging
	// one of the tenant's sub-merchants, with the fee resolved from our store:
	_ = func(ctx context.Context, accountID, subMerchant string) error {
		t, ok := db.get(accountID)
		if !ok {
			return fmt.Errorf("unknown tenant")
		}
		tenantClient := stripe.New(t.AccessToken, stripe.WithFeeResolver(db))
		_, err := tenantClient.ChargeWithFee(ctx, stripe.ChargeParams{
			ConnectedAccount: subMerchant,
			Amount:           stripe.Dollars(20),
		})
		return err
	}

	log.Println("listening on :8080  (/connect, /callback, /fee)")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
