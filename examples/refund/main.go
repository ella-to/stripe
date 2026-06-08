// Command refund demonstrates purchase and full/partial refund flows, including
// subscription cancellation with a refund for the LiteScale "cancel with
// money-back" scenario.
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
		Fee:              &stripe.PlatformFee{Percent: 10}, // LiteScale takes 10%
	})
	if err != nil {
		log.Fatalf("checkout: %v", err)
	}
	fmt.Println("checkout url:", session.URL)

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
			log.Fatalf("full refund: %v", err)
		}
		fmt.Printf("refund %s status: %s\n", refund.ID, refund.Status)

		// Partial refund — customer kept the bike for half the day.
		partial, err := client.RefundPayment(ctx, stripe.RefundParams{
			PaymentIntentID: paymentIntentID,
			Amount:          stripe.Dollars(22.50),
			Reason:          stripe.RefundRequestedByCustomer,
		})
		if err != nil {
			log.Fatalf("partial refund: %v", err)
		}
		fmt.Printf("partial refund %s for %.2f\n", partial.ID, float64(partial.Amount)/100)
	}

	// --- Subscription cancel with refund ---

	// Create a monthly plan and subscribe the customer.
	plan, err := client.CreatePlan(ctx, stripe.PlanParams{
		ProductName: "RentEasy Pro",
		Amount:      stripe.Dollars(19.99),
		Interval:    stripe.Monthly,
	})
	if err != nil {
		log.Fatalf("create plan: %v", err)
	}

	sub, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: customerID,
		PriceID:  plan.ID,
	})
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	fmt.Println("subscribed:", sub.ID)

	// Show the customer when their access ends (now = period end since active).
	until := stripe.SubscriptionAccessUntil(sub)
	fmt.Printf("access until: %s\n", until.Format(time.RFC1123))

	// Cancel at period end (customer keeps access until paid period ends).
	cancelled, err := client.Unsubscribe(ctx, sub.ID, stripe.CancelAtPeriodEnd)
	if err != nil {
		log.Fatalf("cancel at period end: %v", err)
	}
	fmt.Printf("will cancel at: %s\n", stripe.SubscriptionAccessUntil(cancelled).Format(time.RFC1123))

	// Customer changed their mind — re-enable the subscription.
	reactivated, err := client.Resubscribe(ctx, sub.ID)
	if err != nil {
		log.Fatalf("resubscribe: %v", err)
	}
	fmt.Println("resubscribed, status:", reactivated.Status)

	// Cancel immediately AND refund the last payment (money-back guarantee).
	cancelledSub, refund, err := client.UnsubscribeWithRefund(ctx, sub.ID)
	if err != nil {
		log.Fatalf("cancel with refund: %v", err)
	}
	fmt.Println("subscription cancelled:", cancelledSub.Status)
	if refund != nil {
		fmt.Printf("refunded %d %s\n", refund.Amount, refund.Currency)
	}
}
