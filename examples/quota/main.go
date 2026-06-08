// Command quota demonstrates usage based / quota billing: package pricing with
// automatic overage ("1000 requests for $2, then another $2 per 1000"), usage
// reporting, an expiring monthly credit grant, and quota cancellation (voiding
// an active credit grant when a customer cancels their quota plan).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"ella.to/stripe"
)

func main() {
	client := stripe.New(os.Getenv("STRIPE_SECRET_KEY"))
	ctx := context.Background()

	customerID := os.Getenv("STRIPE_CUSTOMER_ID") // cus_...

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
		log.Fatalf("setup metered quota: %v", err)
	}
	fmt.Println("meter:", plan.Meter.ID, "price:", plan.Price.ID)

	// Subscribe the customer to the metered price.
	sub, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: customerID,
		PriceID:  plan.Price.ID,
	})
	if err != nil {
		log.Fatalf("subscribe to metered price: %v", err)
	}
	fmt.Println("metered subscription:", sub.ID)

	// Report usage as the customer consumes the resource. Crossing 1000 rolls
	// into the next $2 package automatically.
	if _, err := client.ReportUsage(ctx, customerID, "api_request", 1500); err != nil {
		log.Fatalf("report usage: %v", err)
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
		log.Fatalf("grant quota: %v", err)
	}
	fmt.Println("credit grant:", grant.ID)

	// When the customer cancels their quota plan, void the remaining credit so
	// it cannot be consumed after they are no longer a paying subscriber.
	voided, err := client.VoidCreditGrant(ctx, grant.ID)
	if err != nil {
		log.Fatalf("void credit grant: %v", err)
	}
	fmt.Println("voided credit grant:", voided.ID)
}
