// Command subscriptions demonstrates monthly/yearly plans, trials, plan swaps,
// yearly discounts via coupons, access timeline, resubscribe, and cancellation.
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

	// Create a monthly and a yearly plan for the same product.
	monthly, err := client.CreatePlan(ctx, stripe.PlanParams{
		ProductName: "Pro",
		Amount:      stripe.Dollars(20), // $20.00 / month
		Interval:    stripe.Monthly,
	})
	if err != nil {
		fatal("create monthly plan", "err", err)
	}

	yearly, err := client.CreatePlan(ctx, stripe.PlanParams{
		ProductName: "Pro",
		Amount:      stripe.Dollars(200), // $200.00 / year (~17% savings)
		Interval:    stripe.Yearly,
	})
	if err != nil {
		fatal("create yearly plan", "err", err)
	}
	slog.Info("plans", "monthly", monthly.ID, "yearly", yearly.ID)

	// A 20%-off coupon for yearly plans. Store the coupon ID and apply it when
	// customers pick the yearly plan.
	yearlyDiscount, err := client.CreateCoupon(ctx, stripe.CouponParams{
		Name:       "Yearly Saver 20%",
		PercentOff: 20,
		Duration:   stripe.CouponOnce,
	})
	if err != nil {
		fatal("create coupon", "err", err)
	}
	slog.Info("yearly coupon", "id", yearlyDiscount.ID)

	// Subscribe with a 14-day trial.
	sub, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer:  customerID,
		PriceID:   monthly.ID,
		TrialDays: 14,
	})
	if err != nil {
		fatal("subscribe", "err", err)
	}
	slog.Info("subscription", "id", sub.ID, "status", sub.Status)
	slog.Info("trial access until", "time", stripe.SubscriptionAccessUntil(sub).Format(time.RFC1123))

	// A "3 month" trial is expressed with a trial end timestamp.
	_, err = client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: customerID,
		PriceID:  yearly.ID,
		TrialEnd: time.Now().AddDate(0, 3, 0),
	})
	if err != nil {
		fatal("subscribe yearly", "err", err)
	}

	// Subscribe to yearly with the discount coupon applied.
	discountedYearly, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: customerID,
		PriceID:  yearly.ID,
		CouponID: yearlyDiscount.ID,
	})
	if err != nil {
		fatal("subscribe with discount", "err", err)
	}
	slog.Info("discounted yearly", "id", discountedYearly.ID)

	// Upgrade the monthly subscription to yearly.
	if _, err := client.SwapPlan(ctx, sub.ID, yearly.ID); err != nil {
		fatal("swap plan", "err", err)
	}

	// Cancel at period end — customer keeps access until their paid period ends.
	cancelled, err := client.Unsubscribe(ctx, sub.ID, stripe.CancelAtPeriodEnd)
	if err != nil {
		fatal("cancel at period end", "err", err)
	}
	slog.Info("access until", "time", stripe.SubscriptionAccessUntil(cancelled).Format(time.RFC1123))

	// Customer changed their mind — reactivate before the period ends.
	reactivated, err := client.Resubscribe(ctx, sub.ID)
	if err != nil {
		fatal("resubscribe", "err", err)
	}
	slog.Info("reactivated", "cancel_at_period_end", reactivated.CancelAtPeriodEnd)

	// List all subscriptions for the customer.
	subs, err := client.ListSubscriptions(ctx, customerID)
	if err != nil {
		fatal("list subscriptions", "err", err)
	}
	slog.Info("subscriptions for customer", "count", len(subs))

	// Cancel immediately (no refund).
	if _, err := client.Unsubscribe(ctx, sub.ID, stripe.CancelImmediately); err != nil {
		fatal("cancel now", "err", err)
	}

	// Retire the discount coupon when the promotion ends.
	if err := client.DeleteCoupon(ctx, yearlyDiscount.ID); err != nil {
		fatal("delete coupon", "err", err)
	}
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
