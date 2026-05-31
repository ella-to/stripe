// Command subscriptions demonstrates monthly/yearly plans, trials, plan swaps
// and cancellation (immediate or at period end).
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

	// Create a monthly and a yearly plan for the same product.
	monthly, err := client.CreatePlan(ctx, stripe.PlanParams{
		ProductName: "Pro",
		Amount:      stripe.Dollars(20), // $20.00 / month
		Interval:    stripe.Monthly,
	})
	if err != nil {
		log.Fatalf("create monthly plan: %v", err)
	}

	yearly, err := client.CreatePlan(ctx, stripe.PlanParams{
		ProductName: "Pro",
		Amount:      stripe.Dollars(200), // $200.00 / year
		Interval:    stripe.Yearly,
	})
	if err != nil {
		log.Fatalf("create yearly plan: %v", err)
	}
	fmt.Println("monthly price:", monthly.ID, "yearly price:", yearly.ID)

	// Subscribe with a 14 day trial.
	sub, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer:  customerID,
		PriceID:   monthly.ID,
		TrialDays: 14,
	})
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	fmt.Println("subscription:", sub.ID, "status:", sub.Status)

	// A "3 month" trial is expressed with a trial end timestamp.
	_, err = client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: customerID,
		PriceID:  yearly.ID,
		TrialEnd: time.Now().AddDate(0, 3, 0),
	})
	if err != nil {
		log.Fatalf("subscribe yearly: %v", err)
	}

	// Upgrade the first subscription from monthly to yearly.
	if _, err := client.SwapPlan(ctx, sub.ID, yearly.ID); err != nil {
		log.Fatalf("swap plan: %v", err)
	}

	// Cancel at period end (customer keeps access until paid period ends).
	if _, err := client.Unsubscribe(ctx, sub.ID, stripe.CancelAtPeriodEnd); err != nil {
		log.Fatalf("cancel at period end: %v", err)
	}

	// Or cancel immediately.
	if _, err := client.Unsubscribe(ctx, sub.ID, stripe.CancelImmediately); err != nil {
		log.Fatalf("cancel now: %v", err)
	}
}
