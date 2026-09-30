// Guide 11: a marketplace with Stripe Connect Express. See README.md.
package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"ella.to/stripe"
)

const addr = "localhost:4242"

// Every seller gets this platform fee: 10% + $0.30 per sale.
var platformFee = stripe.PlatformFee{Percent: 10, Fixed: stripe.Dollars(0.30)}

const productPrice = 2000 // $20.00

type seller struct {
	ID    string // connected account id (acct_...)
	Email string
	Ready bool // finished onboarding, can accept payments
}

type order struct {
	ID      string
	Seller  string
	Status  string // "pending", "paid", "refunded"
	Payment string // PaymentIntent id on the seller's account
}

var (
	mu      sync.Mutex
	sellers = map[string]*seller{}
	orders  = map[string]*order{}
)

func main() {
	client := stripe.New(
		mustEnv("STRIPE_SECRET_KEY"),
		stripe.WithWebhookSecret(mustEnv("STRIPE_WEBHOOK_SECRET")),
	)

	hooks := client.Webhooks(stripe.WithIgnoreAPIVersionMismatch()) // local testing: accept your account's API version

	// Direct charges happen on the seller's account, so this event is a
	// Connect event: ev.Account is the seller (needs --forward-connect-to).
	stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		mu.Lock()
		defer mu.Unlock()
		o, ok := orders[cs.ClientReferenceID]
		if !ok {
			slog.Warn("webhook for unknown order", "session", cs.ID, "account", ev.Account)
			return nil
		}
		if cs.PaymentStatus == "paid" {
			o.Status = "paid"
			if cs.PaymentIntent != nil {
				o.Payment = cs.PaymentIntent.ID
			}
			slog.Info("order paid", "order", o.ID, "seller", ev.Account)
		}
		return nil
	})

	// Sent whenever onboarding progresses (or Stripe needs more information).
	stripe.On(hooks, stripe.EventAccountUpdated, func(ctx context.Context, ev stripe.Event, acct *stripe.Account) error {
		mu.Lock()
		defer mu.Unlock()
		if s, ok := sellers[acct.ID]; ok {
			s.Ready = stripe.AccountReady(acct)
			slog.Info("seller updated", "seller", s.ID, "ready", s.Ready)
		}
		return nil
	})

	mux := http.NewServeMux()
	mux.Handle("POST /webhook", hooks)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprint(w, `<h1>Marketplace</h1><p><a href="/sell">Become a seller</a> · <a href="/orders">Orders</a></p><h2>Sellers</h2><ul>`)
		for _, s := range sellers {
			fmt.Fprintf(w, `<li>%s (ready: %v) — <a href="/seller/%s">seller page</a> · <a href="/store/%s">store</a></li>`, html.EscapeString(s.Email), s.Ready, s.ID, s.ID)
		}
		fmt.Fprint(w, "</ul>")
	})

	// 1. Seller signs up: create an Express account and set their fee.
	mux.HandleFunc("GET /sell", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<h1>Sell with us</h1><form method="POST" action="/sell">
<input name="email" type="email" placeholder="you@example.com" required> <button>Start onboarding</button></form>`)
	})
	mux.HandleFunc("POST /sell", func(w http.ResponseWriter, r *http.Request) {
		acct, err := client.RegisterConnectedAccount(r.Context(), stripe.ConnectedAccountParams{
			Type:         stripe.AccountExpress,
			Email:        r.FormValue("email"),
			Country:      "US",
			Capabilities: []string{"card_payments", "transfers"},
		})
		if err != nil {
			fail(w, "register account", err)
			return
		}
		// Stored on the account's metadata; Checkout picks it up automatically.
		if _, err := client.SetPlatformFee(r.Context(), acct.ID, platformFee); err != nil {
			fail(w, "set platform fee", err)
			return
		}
		mu.Lock()
		sellers[acct.ID] = &seller{ID: acct.ID, Email: r.FormValue("email")}
		mu.Unlock()
		slog.Info("seller registered", "seller", acct.ID)
		http.Redirect(w, r, "/onboard/"+acct.ID, http.StatusSeeOther)
	})

	// 2. Hosted onboarding. Links are single use, so this route also serves as
	// the refresh URL: an expired link just lands here and gets a new one.
	mux.HandleFunc("GET /onboard/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		link, err := client.AccountOnboardingLink(r.Context(), id,
			"http://"+addr+"/onboard/"+id, // refresh URL
			"http://"+addr+"/seller/"+id,  // return URL
		)
		if err != nil {
			fail(w, "onboarding link", err)
			return
		}
		http.Redirect(w, r, link.URL, http.StatusSeeOther)
	})

	// 3. Seller page: returning from onboarding does NOT mean it is finished,
	// so always check the account.
	mux.HandleFunc("GET /seller/{id}", func(w http.ResponseWriter, r *http.Request) {
		acct, err := client.GetConnectedAccount(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, "get account", err)
			return
		}
		ready := stripe.AccountReady(acct)
		mu.Lock()
		if s, ok := sellers[acct.ID]; ok {
			s.Ready = ready
		}
		mu.Unlock()

		fmt.Fprintf(w, "<h1>Seller %s</h1>", acct.ID)
		if !ready {
			fmt.Fprintf(w, `<p>Onboarding is not finished.</p><a href="/onboard/%s">Finish onboarding</a>`, acct.ID)
			return
		}
		fmt.Fprintf(w, `<p>✅ Ready to sell. Fee: %.1f%% + $%.2f per sale.</p>
<p><a href="/store/%s">Your store</a></p>
<form method="POST" action="/seller/%s/dashboard"><button>Open Express dashboard</button></form>`,
			platformFee.Percent, float64(platformFee.Fixed)/100, acct.ID, acct.ID)
	})

	// 4. Express dashboard: payouts, balance, and the seller's own payments.
	mux.HandleFunc("POST /seller/{id}/dashboard", func(w http.ResponseWriter, r *http.Request) {
		link, err := client.ExpressDashboardLink(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, "dashboard link", err)
			return
		}
		http.Redirect(w, r, link.URL, http.StatusSeeOther)
	})

	// 5. Buyer: the store page and checkout as a direct charge on the seller.
	mux.HandleFunc("GET /store/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		fmt.Fprintf(w, `<h1>Store %s</h1><p>Handmade mug — $%.2f</p>
<form method="POST" action="/store/%s/buy"><button>Buy</button></form>
<p>Platform fee on this sale: $%.2f</p>`,
			id, float64(productPrice)/100, id, float64(platformFee.Compute(productPrice))/100)
	})
	mux.HandleFunc("POST /store/{id}/buy", func(w http.ResponseWriter, r *http.Request) {
		sellerID := r.PathValue("id")
		o := &order{ID: fmt.Sprintf("order-%d", time.Now().UnixNano()), Seller: sellerID, Status: "pending"}

		session, err := client.Checkout(r.Context(), stripe.CheckoutParams{
			Cart:              stripe.NewCart("usd").AddItem("Handmade mug", productPrice, 1),
			ConnectedAccount:  sellerID, // fee comes from SetPlatformFee
			ClientReferenceID: o.ID,
			SuccessURL:        "http://" + addr + "/orders",
			CancelURL:         "http://" + addr + "/store/" + sellerID,
		})
		if err != nil {
			fail(w, "create checkout", err)
			return
		}
		mu.Lock()
		orders[o.ID] = o
		mu.Unlock()
		http.Redirect(w, r, session.URL, http.StatusSeeOther)
	})

	// 6. Orders, with a refund button.
	mux.HandleFunc("GET /orders", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprint(w, "<h1>Orders</h1><ul>")
		for _, o := range orders {
			fmt.Fprintf(w, "<li>%s — seller %s — <b>%s</b>", o.ID, o.Seller, o.Status)
			if o.Status == "paid" {
				fmt.Fprintf(w, ` <form style="display:inline" method="POST" action="/orders/%s/refund"><button>Refund</button></form>`, o.ID)
			}
			fmt.Fprint(w, "</li>")
		}
		fmt.Fprint(w, `</ul><a href="/">Home</a>`)
	})
	mux.HandleFunc("POST /orders/{id}/refund", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		o, ok := orders[r.PathValue("id")]
		mu.Unlock()
		if !ok || o.Payment == "" {
			http.Error(w, "order not paid", http.StatusBadRequest)
			return
		}
		refund, err := client.RefundPayment(r.Context(), stripe.RefundParams{
			PaymentIntentID:      o.Payment,
			ConnectedAccount:     o.Seller, // the charge lives on the seller's account
			RefundApplicationFee: true,     // give the platform fee back too
			Reason:               stripe.RefundRequestedByCustomer,
		})
		if err != nil {
			fail(w, "refund", err)
			return
		}
		mu.Lock()
		o.Status = "refunded"
		mu.Unlock()
		slog.Info("order refunded", "order", o.ID, "refund", refund.ID)
		http.Redirect(w, r, "/orders", http.StatusSeeOther)
	})

	slog.Info("listening", "url", "http://"+addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fatal("server stopped", "err", err)
	}
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
