// Command subscriptions demonstrates monthly/yearly plans, trials, plan swaps,
// yearly discounts via coupons, access timeline, resubscribe, and cancellation.
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
		Amount:      stripe.Dollars(200), // $200.00 / year (~17% savings)
		Interval:    stripe.Yearly,
	})
	if err != nil {
		log.Fatalf("create yearly plan: %v", err)
	}
	fmt.Println("monthly price:", monthly.ID, "yearly price:", yearly.ID)

	// SASS platform creates a 20%-off coupon for yearly plans. Store the coupon
	// ID and let customers choose it at checkout.
	yearlyDiscount, err := client.CreateCoupon(ctx, stripe.CouponParams{
		Name:       "Yearly Saver 20%",
		PercentOff: 20,
		Duration:   stripe.CouponOnce,
	})
	if err != nil {
		log.Fatalf("create coupon: %v", err)
	}
	fmt.Println("yearly coupon:", yearlyDiscount.ID)

	// Subscribe with a 14-day trial.
	sub, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer:  customerID,
		PriceID:   monthly.ID,
		TrialDays: 14,
	})
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	fmt.Println("subscription:", sub.ID, "status:", sub.Status)
	fmt.Println("trial access until:", stripe.SubscriptionAccessUntil(sub).Format(time.RFC1123))

	// A "3 month" trial is expressed with a trial end timestamp.
	_, err = client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: customerID,
		PriceID:  yearly.ID,
		TrialEnd: time.Now().AddDate(0, 3, 0),
	})
	if err != nil {
		log.Fatalf("subscribe yearly: %v", err)
	}

	// Subscribe to yearly with the discount coupon applied.
	discountedYearly, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: customerID,
		PriceID:  yearly.ID,
		CouponID: yearlyDiscount.ID,
	})
	if err != nil {
		log.Fatalf("subscribe with discount: %v", err)
	}
	fmt.Println("discounted yearly:", discountedYearly.ID)

	// Upgrade the monthly subscription to yearly.
	if _, err := client.SwapPlan(ctx, sub.ID, yearly.ID); err != nil {
		log.Fatalf("swap plan: %v", err)
	}

	// Cancel at period end — customer keeps access until their paid period ends.
	cancelled, err := client.Unsubscribe(ctx, sub.ID, stripe.CancelAtPeriodEnd)
	if err != nil {
		log.Fatalf("cancel at period end: %v", err)
	}
	fmt.Println("access until:", stripe.SubscriptionAccessUntil(cancelled).Format(time.RFC1123))

	// Customer changed their mind — reactivate before the period ends.
	reactivated, err := client.Resubscribe(ctx, sub.ID)
	if err != nil {
		log.Fatalf("resubscribe: %v", err)
	}
	fmt.Println("reactivated, cancel_at_period_end:", reactivated.CancelAtPeriodEnd)

	// List all subscriptions for the customer.
	subs, err := client.ListSubscriptions(ctx, customerID)
	if err != nil {
		log.Fatalf("list subscriptions: %v", err)
	}
	fmt.Printf("%d subscription(s) for customer\n", len(subs))

	// Cancel immediately (no refund).
	if _, err := client.Unsubscribe(ctx, sub.ID, stripe.CancelImmediately); err != nil {
		log.Fatalf("cancel now: %v", err)
	}

	// Retire the discount coupon when the promotion ends.
	if err := client.DeleteCoupon(ctx, yearlyDiscount.ID); err != nil {
		log.Fatalf("delete coupon: %v", err)
	}
}
