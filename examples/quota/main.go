// Command quota demonstrates usage based / quota billing: package pricing with
// automatic overage ("1000 requests for $2, then another $2 per 1000"), usage
// reporting, an expiring monthly credit grant, and quota cancellation (voiding
// an active credit grant when a customer cancels their quota plan).
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"ella.to/stripe"
)

func main() {
	client := stripe.New(os.Getenv("STRIPE_SECRET_KEY"))
	ctx := context.Background()

	customerID := os.Getenv("STRIPE_CUSTOMER_ID") // cus_...
	if customerID == "" {
		// No customer given: create one with a test card attached.
		cus, err := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
			Email:         "demo@example.com",
			PaymentMethod: "pm_card_visa",
		})
		if err != nil {
			fatal("create customer", "err", err)
		}
		customerID = cus.ID
	}

	// Set up the meter + metered package price in one call:
	//   $2.00 per 1000 API requests, billed monthly, overage automatic.
	plan, err := client.SetupMeteredQuota(ctx, stripe.SetupMeteredQuotaParams{
		ProductName:      "API Requests",
		EventName:        "api_request",
		Aggregation:      stripe.AggSum,
		AmountPerPackage: stripe.Dollars(2),
		PackageSize:      1000,
		Interval:         stripe.Monthly,
	})
	if err != nil {
		fatal("setup metered quota", "err", err)
	}
	slog.Info("metered quota ready", "meter", plan.Meter.ID, "price", plan.Price.ID)

	// Subscribe the customer to the metered price.
	sub, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: customerID,
		PriceID:  plan.Price.ID,
	})
	if err != nil {
		fatal("subscribe to metered price", "err", err)
	}
	slog.Info("metered subscription", "id", sub.ID)

	// Report usage as the customer consumes the resource. Crossing 1000 rolls
	// into the next $2 package automatically.
	if _, err := client.ReportUsage(ctx, customerID, "api_request", 1500); err != nil {
		fatal("report usage", "err", err)
	}

	// Prepaid, expiring monthly quota: grant $2 of credit that expires in 30
	// days. Re-grant each cycle (e.g. from an invoice.paid webhook) to renew.
	grant, err := client.GrantQuota(ctx, stripe.QuotaGrantParams{
		Customer:  customerID,
		Amount:    stripe.Dollars(2),
		ExpiresIn: 30 * 24 * time.Hour,
		Name:      "Monthly included usage",
		PriceIDs:  []string{plan.Price.ID},
	})
	if err != nil {
		fatal("grant quota", "err", err)
	}
	slog.Info("credit grant", "id", grant.ID)

	// When the customer cancels their quota plan, void the remaining credit so
	// it cannot be consumed after they are no longer a paying subscriber.
	voided, err := client.VoidCreditGrant(ctx, grant.ID)
	if err != nil {
		fatal("void credit grant", "err", err)
	}
	slog.Info("voided credit grant", "id", voided.ID)
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
