// Guide 06: free trials, with and without a card up front. See README.md.
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

const (
	addr      = "localhost:4242"
	trialDays = 14
)

// user is what your database would store (a single always-logged-in demo user).
type user struct {
	ID         string
	Email      string
	CustomerID string
	Sub        *stripe.Subscription
}

var (
	mu   sync.Mutex
	demo = &user{ID: "user-1", Email: "trial@example.com"}
)

func main() {
	client := stripe.New(
		mustEnv("STRIPE_SECRET_KEY"),
		stripe.WithWebhookSecret(mustEnv("STRIPE_WEBHOOK_SECRET")),
	)

	plan, err := client.EnsurePlan(context.Background(), stripe.PlanParams{
		LookupKey: "guide_trial_pro_monthly", ProductName: "Pro (trial guide)",
		Amount: stripe.Dollars(20), Interval: stripe.Monthly,
	})
	if err != nil {
		fatal("ensure plan", "err", err)
	}

	hooks := client.Webhooks(stripe.WithIgnoreAPIVersionMismatch()) // local testing: accept your account's API version

	// Status changes: trialing → active (paid), or → canceled (no card added).
	save := func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
		saveSubscription(ev.Type, sub)
		return nil
	}
	stripe.On(hooks, stripe.EventCustomerSubscriptionCreated, save)
	stripe.On(hooks, stripe.EventCustomerSubscriptionUpdated, save)
	stripe.On(hooks, stripe.EventCustomerSubscriptionDeleted, save)

	// Sent 3 days before the trial ends: remind the customer.
	stripe.On(hooks, stripe.EventCustomerSubscriptionTrialWillEnd, func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
		hasCard := sub.DefaultPaymentMethod != nil
		slog.Info("send reminder email: your trial ends soon",
			"subscription", sub.ID, "trial_end", time.Unix(sub.TrialEnd, 0).Format(time.DateOnly), "has_card", hasCard)
		return nil
	})

	mux := http.NewServeMux()
	mux.Handle("POST /webhook", hooks)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<h1>Pro — $20 / month</h1>
<form method="POST" action="/trial?card=yes"><button>Start %[1]d-day trial (card required)</button></form>
<form method="POST" action="/trial?card=no"><button>Start %[1]d-day trial (no card)</button></form>
<p><a href="/account">Account</a></p>`, trialDays)
	})

	mux.HandleFunc("POST /trial", func(w http.ResponseWriter, r *http.Request) {
		customerID, err := ensureCustomer(r.Context(), client)
		if err != nil {
			slog.Error("create customer", "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		session, err := client.CheckoutSubscription(r.Context(), stripe.SubscriptionCheckoutParams{
			PriceID:           plan.ID,
			Customer:          customerID,
			ClientReferenceID: demo.ID,
			Metadata:          map[string]string{"user_id": demo.ID},
			TrialDays:         trialDays,
			// No card: Checkout only asks for an email. If no card is added by
			// the end of the trial, Stripe cancels the subscription.
			TrialWithoutCard: r.URL.Query().Get("card") == "no",
			SuccessURL:       "http://" + addr + "/account",
			CancelURL:        "http://" + addr + "/",
		})
		if err != nil {
			slog.Error("create checkout", "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		http.Redirect(w, r, session.URL, http.StatusSeeOther)
	})

	mux.HandleFunc("GET /account", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sub := demo.Sub
		mu.Unlock()
		fmt.Fprint(w, "<h1>Account</h1>")
		if sub == nil {
			fmt.Fprint(w, `<p>No trial yet (if you just signed up, refresh in a second).</p><a href="/">Start a trial</a>`)
			return
		}
		fmt.Fprintf(w, "<p>Status: <b>%s</b> · access: <b>%v</b> · until: %s · card on file: %v</p><a href=\"/\">Back</a>",
			sub.Status, stripe.HasAccess(sub),
			stripe.SubscriptionAccessUntil(sub).Format(time.DateOnly), sub.DefaultPaymentMethod != nil)
	})

	slog.Info("listening", "url", "http://"+addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fatal("server stopped", "err", err)
	}
}

func ensureCustomer(ctx context.Context, client *stripe.Client) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if demo.CustomerID != "" {
		return demo.CustomerID, nil
	}
	cus, err := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
		Email:    demo.Email,
		Metadata: map[string]string{"user_id": demo.ID},
	})
	if err != nil {
		return "", err
	}
	demo.CustomerID = cus.ID
	return cus.ID, nil
}

func saveSubscription(event stripe.EventType, sub *stripe.Subscription) {
	mu.Lock()
	defer mu.Unlock()
	if sub.Customer == nil || sub.Customer.ID != demo.CustomerID {
		slog.Warn("subscription for unknown customer", "event", event, "subscription", sub.ID, "status", sub.Status)
		return
	}
	demo.Sub = sub
	slog.Info("subscription updated", "event", event, "subscription", sub.ID,
		"status", sub.Status, "has_access", stripe.HasAccess(sub))
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
