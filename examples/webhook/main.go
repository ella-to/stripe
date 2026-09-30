// Command webhook demonstrates managing webhook endpoints and serving a typed,
// generic webhook dispatcher that decodes each event into the right Go type and
// runs handlers in goroutines while acknowledging Stripe immediately.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"ella.to/stripe"
)

func main() {
	client := stripe.New(
		os.Getenv("STRIPE_SECRET_KEY"),
		stripe.WithWebhookSecret(os.Getenv("STRIPE_WEBHOOK_SECRET")),
	)
	ctx := context.Background()

	// Create a webhook endpoint on demand.
	if os.Getenv("CREATE_ENDPOINT") == "1" {
		ep, err := client.CreateWebhookEndpoint(ctx, stripe.WebhookEndpointParams{
			URL:         "https://app.example.com/stripe/webhook",
			Description: "Primary endpoint",
			Events: []string{
				"invoice.paid",
				"customer.subscription.deleted",
				"checkout.session.completed",
			},
		})
		if err != nil {
			fatal("create endpoint", "err", err)
		}
		// Store ep.Secret securely; it verifies deliveries for this endpoint.
		slog.Info("created endpoint", "id", ep.ID, "secret", ep.Secret)
	}

	// Build the dispatcher and register strongly typed handlers. The object
	// passed to each handler is decoded and validated from the event payload.
	// Errors (bad signatures, failing handlers) are logged with slog.Default();
	// pass stripe.WithLogger to stripe.New, or stripe.WithErrorHandler here, to customise.
	d := client.Webhooks()

	stripe.On(d, stripe.EventInvoicePaid, func(ctx context.Context, ev stripe.Event, inv *stripe.Invoice) error {
		slog.Info("invoice paid", "id", inv.ID, "amount", inv.AmountPaid)
		// Heavy work here is fine: it runs in its own goroutine and Stripe has
		// already received its 200.
		return nil
	})

	stripe.On(d, stripe.EventCustomerSubscriptionDeleted, func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
		slog.Info("subscription canceled", "id", sub.ID, "customer", sub.Customer.ID)
		return nil
	})

	stripe.On(d, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		slog.Info("checkout completed", "id", cs.ID, "payment_status", cs.PaymentStatus)
		return nil
	})

	http.Handle("/stripe/webhook", d)
	slog.Info("listening", "addr", ":8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fatal("server stopped", "err", err)
	}
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
