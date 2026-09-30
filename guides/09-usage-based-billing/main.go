// Guide 09: bill per API call with a usage meter. See README.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"ella.to/stripe"
)

const eventName = "guide_api_call" // what your code reports usage against

func main() {
	client := stripe.New(mustEnv("STRIPE_SECRET_KEY"))
	ctx := context.Background()

	// 1. Meter + price: $2 per started block of 1,000 calls. Safe to re-run:
	//    the meter (by event name) and price (by lookup key) are reused.
	plan, err := client.SetupMeteredQuota(ctx, stripe.SetupMeteredQuotaParams{
		ProductName:      "API calls",
		EventName:        eventName,
		AmountPerPackage: stripe.Dollars(2),
		PackageSize:      1000,
		Interval:         stripe.Monthly,
		LookupKey:        "guide_api_calls_monthly",
	})
	if err != nil {
		fatal("setup metered quota", "err", err)
	}
	slog.Info("metered plan ready", "meter", plan.Meter.ID, "price", plan.Price.ID)

	// 2. A customer with a test card, subscribed to the metered price.
	cus, err := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
		Email:         "usage@example.com",
		PaymentMethod: "pm_card_visa",
	})
	if err != nil {
		fatal("create customer", "err", err)
	}
	sub, err := client.Subscribe(ctx, stripe.SubscribeParams{Customer: cus.ID, PriceID: plan.Price.ID})
	if err != nil {
		fatal("subscribe", "err", err)
	}
	slog.Info("subscribed", "customer", cus.ID, "subscription", sub.ID, "status", sub.Status)

	// 3. Report usage as it happens. The Identifier makes retries safe:
	//    Stripe drops a second event with the same identifier.
	for i, calls := range []int64{400, 700, 350} {
		_, err := client.ReportUsageEvent(ctx, stripe.UsageEvent{
			Customer:   cus.ID,
			EventName:  eventName,
			Value:      calls,
			Identifier: fmt.Sprintf("%s-batch-%d", sub.ID, i),
		})
		if err != nil {
			fatal("report usage", "err", err)
		}
		slog.Info("reported usage", "calls", calls)
	}
	// 1,450 calls → 2 blocks → $4.00 on the next invoice.

	// 4. Preview the bill. Meter events are aggregated asynchronously, so give
	//    Stripe a moment; it can still show less than reported right away.
	time.Sleep(5 * time.Second)
	inv, err := client.UpcomingInvoice(ctx, sub.ID)
	if err != nil {
		fatal("upcoming invoice", "err", err)
	}
	slog.Info("next invoice so far", "amount_due", fmt.Sprintf("$%.2f", float64(inv.AmountDue)/100))

	// 5. Prepaid credit: $2 (1,000 calls) that expires in 30 days. Usage eats
	//    the credit first; only the rest is charged.
	grant, err := client.GrantQuota(ctx, stripe.QuotaGrantParams{
		Customer:  cus.ID,
		Amount:    stripe.Dollars(2),
		ExpiresIn: 30 * 24 * time.Hour,
		Name:      "Included monthly calls",
		PriceIDs:  []string{plan.Price.ID},
	})
	if err != nil {
		fatal("grant quota", "err", err)
	}
	slog.Info("granted credit", "grant", grant.ID)

	// 6. Customer leaves: remove unused credit, then cancel.
	if _, err := client.VoidCreditGrant(ctx, grant.ID); err != nil {
		fatal("void credit grant", "err", err)
	}
	slog.Info("voided credit", "grant", grant.ID)

	if _, err := client.Unsubscribe(ctx, sub.ID, stripe.CancelImmediately); err != nil {
		fatal("unsubscribe", "err", err)
	}
	slog.Info("subscription cancelled", "subscription", sub.ID)
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
