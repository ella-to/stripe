// Guide 03: sell a single product with hosted Checkout. See README.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"ella.to/stripe"
)

const addr = "localhost:4242"

// order is what your database would store.
type order struct {
	ID      string
	Status  string // "pending", "paid", "failed", "expired"
	Session string // Checkout session id
	Payment string // PaymentIntent id, needed for refunds
}

var (
	mu     sync.Mutex
	orders = map[string]*order{}
)

func main() {
	client := stripe.New(
		mustEnv("STRIPE_SECRET_KEY"),
		stripe.WithWebhookSecret(mustEnv("STRIPE_WEBHOOK_SECRET")),
	)

	// Webhooks: the only reliable place to fulfil an order.
	hooks := client.Webhooks(stripe.WithIgnoreAPIVersionMismatch()) // local testing: accept your account's API version
	stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		// Card payments are "paid" here. Delayed methods (bank debits) are
		// "unpaid" until checkout.session.async_payment_succeeded.
		if cs.PaymentStatus == "paid" {
			markOrder(cs, "paid")
		}
		return nil
	})
	stripe.On(hooks, stripe.EventCheckoutSessionAsyncPaymentSucceeded, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		markOrder(cs, "paid")
		return nil
	})
	stripe.On(hooks, stripe.EventCheckoutSessionAsyncPaymentFailed, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		markOrder(cs, "failed")
		return nil
	})
	stripe.On(hooks, stripe.EventCheckoutSessionExpired, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		markOrder(cs, "expired")
		return nil
	})

	mux := http.NewServeMux()
	mux.Handle("POST /webhook", hooks)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<h1>Stripe T-Shirt — $25.00</h1>
<form method="POST" action="/buy"><button>Buy now</button></form>
<p><a href="/orders">Orders</a></p>`)
	})

	mux.HandleFunc("POST /buy", func(w http.ResponseWriter, r *http.Request) {
		o := &order{ID: fmt.Sprintf("order-%d", time.Now().UnixNano()), Status: "pending"}

		cart := stripe.NewCart("usd").AddItem("Stripe T-Shirt", stripe.Dollars(25), 1)
		session, err := client.Checkout(r.Context(), stripe.CheckoutParams{
			Cart:              cart,
			ClientReferenceID: o.ID, // comes back in the webhook
			SuccessURL:        "http://" + addr + "/success?session_id={CHECKOUT_SESSION_ID}",
			CancelURL:         "http://" + addr + "/",
		})
		if err != nil {
			slog.Error("create checkout", "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		o.Session = session.ID
		mu.Lock()
		orders[o.ID] = o
		mu.Unlock()
		http.Redirect(w, r, session.URL, http.StatusSeeOther)
	})

	// The success page only *shows* the result; the webhook does the work.
	mux.HandleFunc("GET /success", func(w http.ResponseWriter, r *http.Request) {
		cs, err := client.GetCheckoutSession(r.Context(), r.URL.Query().Get("session_id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "<h1>Thanks!</h1><p>Order %s — payment %s</p><ul>", cs.ClientReferenceID, cs.PaymentStatus)
		if cs.LineItems != nil {
			for _, li := range cs.LineItems.Data {
				fmt.Fprintf(w, "<li>%d × %s — $%.2f</li>", li.Quantity, li.Description, float64(li.AmountTotal)/100)
			}
		}
		fmt.Fprint(w, `</ul><a href="/orders">Orders</a>`)
	})

	mux.HandleFunc("GET /orders", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprint(w, "<h1>Orders</h1><ul>")
		for _, o := range orders {
			fmt.Fprintf(w, "<li>%s — <b>%s</b> %s</li>", o.ID, o.Status, o.Payment)
		}
		fmt.Fprint(w, `</ul><a href="/">Shop</a>`)
	})

	slog.Info("listening", "url", "http://"+addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fatal("server stopped", "err", err)
	}
}

func markOrder(cs *stripe.CheckoutSession, status string) {
	mu.Lock()
	defer mu.Unlock()
	o, ok := orders[cs.ClientReferenceID]
	if !ok {
		// e.g. `stripe trigger` events, or an order from a previous run.
		slog.Warn("webhook for unknown order", "session", cs.ID, "status", status)
		return
	}
	o.Status = status
	if cs.PaymentIntent != nil {
		o.Payment = cs.PaymentIntent.ID
	}
	slog.Info("order updated", "order", o.ID, "status", status)
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
