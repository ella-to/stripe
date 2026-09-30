// Guide 10: add tax at checkout, with your own tax rates or Stripe Tax. See README.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"ella.to/stripe"
)

const addr = "localhost:4242"

func main() {
	client := stripe.New(
		mustEnv("STRIPE_SECRET_KEY"),
		stripe.WithWebhookSecret(mustEnv("STRIPE_WEBHOOK_SECRET")),
	)
	ctx := context.Background()

	// 1. A tax rate you manage yourself. Reused across restarts.
	salesTax, err := ensureTaxRate(ctx, client, stripe.TaxRateParams{
		DisplayName:  "Sales Tax",
		Percentage:   8.25,
		Country:      "US",
		State:        "TX",
		Jurisdiction: "Texas",
		TaxType:      "sales_tax",
	})
	if err != nil {
		fatal("tax rate", "err", err)
	}
	slog.Info("tax rate ready", "id", salesTax.ID, "percent", salesTax.Percentage)

	// 2. The webhook tells you what was actually charged, tax included.
	hooks := client.Webhooks(stripe.WithIgnoreAPIVersionMismatch()) // local testing: accept your account's API version
	stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		var tax int64
		if cs.TotalDetails != nil {
			tax = cs.TotalDetails.AmountTax
		}
		slog.Info("paid", "session", cs.ID, "total", cs.AmountTotal, "tax", tax, "status", cs.PaymentStatus)
		return nil
	})

	mux := http.NewServeMux()
	mux.Handle("POST /webhook", hooks)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<h1>Desk Lamp — $40.00 + tax</h1>
<form method="POST" action="/buy/manual"><button>Buy (8.25% sales tax rate)</button></form>
<form method="POST" action="/buy/automatic"><button>Buy (Stripe Tax, by address)</button></form>`)
	})

	// 3a. Manual: every line gets the tax rate.
	mux.HandleFunc("POST /buy/manual", func(w http.ResponseWriter, r *http.Request) {
		cart := stripe.NewCart("usd").
			AddItem("Desk Lamp", stripe.Dollars(40), 1).
			WithTaxRates(salesTax.ID)
		checkout(w, r, client, cart)
	})

	// 3b. Automatic: Stripe Tax works out the tax from the shipping address.
	mux.HandleFunc("POST /buy/automatic", func(w http.ResponseWriter, r *http.Request) {
		cart := stripe.NewCart("usd").
			AddItem("Desk Lamp", stripe.Dollars(40), 1).
			ShipTo("US", "CA", "GB", "DE").
			WithAutomaticTax()
		checkout(w, r, client, cart)
	})

	mux.HandleFunc("GET /success", func(w http.ResponseWriter, r *http.Request) {
		cs, err := client.GetCheckoutSession(r.Context(), r.URL.Query().Get("session_id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var tax int64
		if cs.TotalDetails != nil {
			tax = cs.TotalDetails.AmountTax
		}
		fmt.Fprintf(w, `<h1>Thanks!</h1><p>Total $%.2f, of which tax $%.2f</p><a href="/">Back</a>`,
			float64(cs.AmountTotal)/100, float64(tax)/100)
	})

	slog.Info("listening", "url", "http://"+addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fatal("server stopped", "err", err)
	}
}

func checkout(w http.ResponseWriter, r *http.Request, client *stripe.Client, cart *stripe.Cart) {
	session, err := client.Checkout(r.Context(), stripe.CheckoutParams{
		Cart:       cart,
		SuccessURL: "http://" + addr + "/success?session_id={CHECKOUT_SESSION_ID}",
		CancelURL:  "http://" + addr + "/",
	})
	if err != nil {
		// The usual cause for the automatic button: Stripe Tax isn't activated yet.
		slog.Error("create checkout", "err", err)
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `<h1>Stripe said no</h1><p>%s</p>
<p>Using Stripe Tax? Activate it first: Dashboard → Tax → set your origin address.</p><a href="/">Back</a>`, err)
		return
	}
	http.Redirect(w, r, session.URL, http.StatusSeeOther)
}

// ensureTaxRate reuses an active tax rate with the same name and percentage,
// or creates it. Tax rates can't be edited or deleted, only archived.
func ensureTaxRate(ctx context.Context, client *stripe.Client, p stripe.TaxRateParams) (*stripe.TaxRate, error) {
	rates, err := client.ListTaxRates(ctx, true)
	if err != nil {
		return nil, err
	}
	for _, tr := range rates {
		if tr.DisplayName == p.DisplayName && tr.Percentage == p.Percentage && tr.Inclusive == p.Inclusive {
			return tr, nil
		}
	}
	return client.CreateTaxRate(ctx, p)
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
