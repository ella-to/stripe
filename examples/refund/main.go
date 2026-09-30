// Command refund demonstrates purchase and full/partial refund flows, including
// subscription cancellation with a refund (a "money-back guarantee").
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
	storeAccountID := os.Getenv("STRIPE_STORE_ACCOUNT_ID") // acct_...

	// --- One-off purchase with a platform fee, then a full refund ---

	// The end user checks out equipment rental on the store's behalf.
	cart := stripe.NewCart("usd").
		AddItem("Mountain Bike – 1 day rental", stripe.Dollars(45), 1)

	session, err := client.Checkout(ctx, stripe.CheckoutParams{
		Cart:             cart,
		ConnectedAccount: storeAccountID,
		SuccessURL:       "https://rentapp.example.com/success",
		CancelURL:        "https://rentapp.example.com/cancel",
		Customer:         customerID,
		Fee:              &stripe.PlatformFee{Percent: 10}, // the platform takes 10%
	})
	if err != nil {
		fatal("checkout", "err", err)
	}
	slog.Info("checkout", "url", session.URL)

	// Later, when checkout.session.completed fires, you have a PaymentIntent id.
	// Simulate the scenario where the customer cancels within the refund window:
	paymentIntentID := os.Getenv("STRIPE_PAYMENT_INTENT_ID") // pi_...
	if paymentIntentID != "" {
		// Full refund (amount 0 = full).
		refund, err := client.RefundPayment(ctx, stripe.RefundParams{
			PaymentIntentID: paymentIntentID,
			Reason:          stripe.RefundRequestedByCustomer,
		})
		if err != nil {
			fatal("full refund", "err", err)
		}
		slog.Info("refund", "id", refund.ID, "status", refund.Status)

		// Partial refund — customer kept the bike for half the day.
		partial, err := client.RefundPayment(ctx, stripe.RefundParams{
			PaymentIntentID: paymentIntentID,
			Amount:          stripe.Dollars(22.50),
			Reason:          stripe.RefundRequestedByCustomer,
		})
		if err != nil {
			fatal("partial refund", "err", err)
		}
		slog.Info("partial refund", "id", partial.ID, "amount", partial.Amount)
	}

	// --- Subscription cancel with refund ---

	// Create a monthly plan and subscribe the customer.
	plan, err := client.CreatePlan(ctx, stripe.PlanParams{
		ProductName: "RentEasy Pro",
		Amount:      stripe.Dollars(19.99),
		Interval:    stripe.Monthly,
	})
	if err != nil {
		fatal("create plan", "err", err)
	}

	sub, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: customerID,
		PriceID:  plan.ID,
	})
	if err != nil {
		fatal("subscribe", "err", err)
	}
	slog.Info("subscribed", "id", sub.ID)

	// Show the customer when their access ends (now = period end since active).
	until := stripe.SubscriptionAccessUntil(sub)
	slog.Info("access until", "time", until.Format(time.RFC1123))

	// Cancel at period end (customer keeps access until paid period ends).
	cancelled, err := client.Unsubscribe(ctx, sub.ID, stripe.CancelAtPeriodEnd)
	if err != nil {
		fatal("cancel at period end", "err", err)
	}
	slog.Info("will cancel", "at", stripe.SubscriptionAccessUntil(cancelled).Format(time.RFC1123))

	// Customer changed their mind — re-enable the subscription.
	reactivated, err := client.Resubscribe(ctx, sub.ID)
	if err != nil {
		fatal("resubscribe", "err", err)
	}
	slog.Info("resubscribed", "status", reactivated.Status)

	// Cancel immediately AND refund the last payment (money-back guarantee).
	cancelledSub, refund, err := client.UnsubscribeWithRefund(ctx, sub.ID)
	if err != nil {
		fatal("cancel with refund", "err", err)
	}
	slog.Info("subscription cancelled", "status", cancelledSub.Status)
	if refund != nil {
		slog.Info("refunded", "amount", refund.Amount, "currency", refund.Currency)
	}
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
