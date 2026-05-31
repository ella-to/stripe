// Command webhook demonstrates managing webhook endpoints and serving a typed,
// generic webhook dispatcher that decodes each event into the right Go type and
// runs handlers in goroutines while acknowledging Stripe immediately.
package main

import (
	"context"
	"log"
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
			log.Fatalf("create endpoint: %v", err)
		}
		// Store ep.Secret securely; it verifies deliveries for this endpoint.
		log.Println("created endpoint", ep.ID, "secret:", ep.Secret)
	}

	// Build the dispatcher and register strongly typed handlers. The object
	// passed to each handler is decoded and validated from the event payload.
	d := client.Webhooks(stripe.WithErrorHandler(func(ev stripe.Event, err error) {
		log.Printf("webhook handler error for %s: %v", ev.Type, err)
	}))

	stripe.On(d, stripe.EventInvoicePaid, func(ctx context.Context, ev stripe.Event, inv *stripe.Invoice) error {
		log.Printf("invoice %s paid, amount=%d", inv.ID, inv.AmountPaid)
		// Heavy work here is fine: it runs in its own goroutine and Stripe has
		// already received its 200.
		return nil
	})

	stripe.On(d, stripe.EventCustomerSubscriptionDeleted, func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
		log.Printf("subscription %s canceled for customer %s", sub.ID, sub.Customer.ID)
		return nil
	})

	stripe.On(d, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		log.Printf("checkout %s completed, payment_status=%s", cs.ID, cs.PaymentStatus)
		return nil
	})

	http.Handle("/stripe/webhook", d)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
