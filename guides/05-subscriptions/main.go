// Guide 05: sell a monthly/yearly subscription with hosted Checkout, gate a
// feature on it, and let the customer manage billing. See README.md.
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

// user is what your database would store. The guide has a single demo user
// that is always "logged in".
type user struct {
	ID         string
	Email      string
	CustomerID string               // Stripe customer, created on first checkout
	Sub        *stripe.Subscription // latest subscription state from webhooks
}

var (
	mu   sync.Mutex
	demo = &user{ID: "user-1", Email: "demo@example.com"}
)

func main() {
	client := stripe.New(
		mustEnv("STRIPE_SECRET_KEY"),
		stripe.WithWebhookSecret(mustEnv("STRIPE_WEBHOOK_SECRET")),
	)
	ctx := context.Background()

	// 1. Plans. EnsurePlan finds the price by lookup key or creates it, so this
	//    is safe to run on every start.
	monthly, err := client.EnsurePlan(ctx, stripe.PlanParams{
		LookupKey: "guide_pro_monthly", ProductName: "Pro",
		Amount: stripe.Dollars(20), Interval: stripe.Monthly,
	})
	if err != nil {
		fatal("ensure monthly plan", "err", err)
	}
	yearly, err := client.EnsurePlan(ctx, stripe.PlanParams{
		LookupKey: "guide_pro_yearly", ProductID: monthly.Product.ID,
		Amount: stripe.Dollars(200), Interval: stripe.Yearly,
	})
	if err != nil {
		fatal("ensure yearly plan", "err", err)
	}
	slog.Info("plans ready", "monthly", monthly.ID, "yearly", yearly.ID)

	// 2. Customer portal. A new test account has no default portal
	//    configuration, so create one that allows switching between the plans.
	portal, err := client.CreatePortalConfiguration(ctx, stripe.PortalConfigParams{
		Headline:                 "Manage your Pro subscription",
		AllowCancel:              true,
		AllowUpdatePaymentMethod: true,
		AllowInvoiceHistory:      true,
		SwitchPrices:             []string{monthly.ID, yearly.ID},
	})
	if err != nil {
		fatal("create portal configuration", "err", err)
	}

	// 3. Webhooks keep your copy of the subscription in sync. Changes made in
	//    Checkout, the portal or the Dashboard all arrive here.
	hooks := client.Webhooks(stripe.WithIgnoreAPIVersionMismatch()) // local testing: accept your account's API version
	stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		if cs.ClientReferenceID != demo.ID || cs.Subscription == nil {
			slog.Warn("checkout for unknown user", "session", cs.ID)
			return nil
		}
		sub, err := client.GetSubscription(ctx, cs.Subscription.ID)
		if err != nil {
			return err // logged by the dispatcher
		}
		saveSubscription(sub)
		return nil
	})
	syncSub := func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
		saveSubscription(sub)
		return nil
	}
	stripe.On(hooks, stripe.EventCustomerSubscriptionCreated, syncSub)
	stripe.On(hooks, stripe.EventCustomerSubscriptionUpdated, syncSub)
	stripe.On(hooks, stripe.EventCustomerSubscriptionDeleted, syncSub)

	mux := http.NewServeMux()
	mux.Handle("POST /webhook", hooks)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<h1>Pro</h1>
<form method="POST" action="/subscribe"><input type="hidden" name="price" value="%s"><button>$20 / month</button></form>
<form method="POST" action="/subscribe"><input type="hidden" name="price" value="%s"><button>$200 / year</button></form>
<p><a href="/account">Account</a> · <a href="/pro-feature">Pro feature</a></p>`, monthly.ID, yearly.ID)
	})

	// 4. Subscribe: send the user to Checkout in subscription mode.
	mux.HandleFunc("POST /subscribe", func(w http.ResponseWriter, r *http.Request) {
		price := r.FormValue("price")
		if price != monthly.ID && price != yearly.ID {
			http.Error(w, "unknown plan", http.StatusBadRequest)
			return
		}
		customerID, err := ensureCustomer(r.Context(), client)
		if err != nil {
			slog.Error("create customer", "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		session, err := client.CheckoutSubscription(r.Context(), stripe.SubscriptionCheckoutParams{
			PriceID:           price,
			Customer:          customerID,
			ClientReferenceID: demo.ID,
			Metadata:          map[string]string{"user_id": demo.ID}, // also lands on the subscription
			SuccessURL:        "http://" + addr + "/account?session_id={CHECKOUT_SESSION_ID}",
			CancelURL:         "http://" + addr + "/",
		})
		if err != nil {
			slog.Error("create checkout", "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		http.Redirect(w, r, session.URL, http.StatusSeeOther)
	})

	// 5. Account page: read your own copy, never call Stripe on every request.
	mux.HandleFunc("GET /account", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sub := demo.Sub
		mu.Unlock()
		fmt.Fprint(w, "<h1>Account</h1>")
		if sub == nil {
			fmt.Fprint(w, `<p>No subscription yet (if you just paid, refresh in a second).</p><a href="/">See plans</a>`)
			return
		}
		fmt.Fprintf(w, "<p>Status: <b>%s</b> · access: <b>%v</b> · until: %s · cancels at period end: %v</p>",
			sub.Status, stripe.HasAccess(sub),
			stripe.SubscriptionAccessUntil(sub).Format(time.DateOnly), sub.CancelAtPeriodEnd)
		fmt.Fprint(w, `<form method="POST" action="/billing"><button>Manage billing</button></form>
<p><a href="/pro-feature">Pro feature</a> · <a href="/">Plans</a></p>`)
	})

	// 6. Billing portal: switch plan, update card, cancel, download invoices.
	mux.HandleFunc("POST /billing", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		customerID := demo.CustomerID
		mu.Unlock()
		if customerID == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		ps, err := client.CustomerPortal(r.Context(), stripe.PortalParams{
			Customer:        customerID,
			ReturnURL:       "http://" + addr + "/account",
			ConfigurationID: portal.ID,
		})
		if err != nil {
			slog.Error("create portal session", "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		http.Redirect(w, r, ps.URL, http.StatusSeeOther)
	})

	// 7. Gate features on the subscription.
	mux.HandleFunc("GET /pro-feature", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ok := stripe.HasAccess(demo.Sub)
		mu.Unlock()
		if !ok {
			http.Error(w, "Pro subscription required", http.StatusPaymentRequired)
			return
		}
		fmt.Fprint(w, "<h1>🎉 Welcome to Pro</h1>")
	})

	slog.Info("listening", "url", "http://"+addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fatal("server stopped", "err", err)
	}
}

// ensureCustomer creates the demo user's Stripe customer on first use.
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
	slog.Info("created customer", "user", demo.ID, "customer", cus.ID)
	return cus.ID, nil
}

func saveSubscription(sub *stripe.Subscription) {
	mu.Lock()
	defer mu.Unlock()
	if sub.Customer == nil || sub.Customer.ID != demo.CustomerID {
		// e.g. `stripe trigger` events, or a customer from a previous run.
		slog.Warn("subscription for unknown customer", "subscription", sub.ID, "status", sub.Status)
		return
	}
	demo.Sub = sub
	slog.Info("subscription updated", "user", demo.ID, "subscription", sub.ID,
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
