// Command saas demonstrates using ella.to/stripe for a standard SaaS
// application that does NOT use Stripe Connect. No connected accounts, no
// platform fees — just a single Stripe account handling subscriptions,
// quota / usage-based billing, one-off purchases, and refunds.
//
// Authentication can be either a plain secret key or an OAuth access token
// obtained after the user connected their Stripe account via OAuth. Both are
// shown; comment out whichever you don't need.
//
// Environment variables:
//
//	STRIPE_SECRET_KEY          plain API key  (sk_test_... or sk_live_...)
//	STRIPE_ACCESS_TOKEN        OAuth access token (alternative to secret key)
//	STRIPE_ACCOUNT_ID          Stripe user ID from the OAuth token  (acct_...)
//	STRIPE_CUSTOMER_ID         existing customer to use (optional)
//	STRIPE_PAYMENT_INTENT_ID   existing PaymentIntent to refund (optional demo)
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
	ctx := context.Background()

	// ── Authentication ────────────────────────────────────────────────────────
	//
	// Option A — secret key (most common, works for your own Stripe account).
	client := stripe.New(os.Getenv("STRIPE_SECRET_KEY"))

	// Option B — OAuth access token. Use this when your account was connected
	// via the Stripe Connect OAuth flow and you stored the access_token.
	// Uncomment the block below and comment out Option A.
	//
	//   tok := &stripe.OAuthToken{
	//       AccessToken:  os.Getenv("STRIPE_ACCESS_TOKEN"),
	//       StripeUserID: os.Getenv("STRIPE_ACCOUNT_ID"),
	//   }
	//   client = stripe.NewFromOAuthToken(tok)

	// ── Customer ──────────────────────────────────────────────────────────────
	//
	// A Customer record ties together subscriptions, invoices, and payment
	// methods. Create one per user at signup and store their cus_... id.
	customerID := os.Getenv("STRIPE_CUSTOMER_ID")
	if customerID == "" {
		cus, err := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
			Email:    "alice@example.com",
			Name:     "Alice",
			Metadata: map[string]string{"app_user_id": "u-42"},
		})
		if err != nil {
			log.Fatalf("create customer: %v", err)
		}
		customerID = cus.ID
		fmt.Println("customer:", customerID)
	}

	// ── Subscription billing ──────────────────────────────────────────────────
	//
	// Create a monthly plan and subscribe the customer. A 14-day trial is
	// included; no card is charged until the trial ends.
	plan, err := client.CreatePlan(ctx, stripe.PlanParams{
		ProductName: "Pro",
		Amount:      stripe.Dollars(29),
		Interval:    stripe.Monthly,
	})
	if err != nil {
		log.Fatalf("create plan: %v", err)
	}
	fmt.Println("plan:", plan.ID)

	sub, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer:  customerID,
		PriceID:   plan.ID,
		TrialDays: 14,
	})
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	fmt.Println("subscription:", sub.ID,
		"| access until:", stripe.SubscriptionAccessUntil(sub).Format(time.RFC1123))

	// ── Quota / usage-based billing ───────────────────────────────────────────
	//
	// "1 000 API requests cost $2; every additional 1 000 cost another $2."
	// SetupMeteredQuota wires up the Billing Meter + package price in one call.
	quota, err := client.SetupMeteredQuota(ctx, stripe.SetupMeteredQuotaParams{
		ProductName:      "API Requests",
		EventName:        "api_request",
		AmountPerPackage: stripe.Dollars(2),
		PackageSize:      1000,
		Interval:         stripe.Monthly,
	})
	if err != nil {
		log.Fatalf("setup quota: %v", err)
	}
	fmt.Println("quota price:", quota.Price.ID)

	if _, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: customerID,
		PriceID:  quota.Price.ID,
	}); err != nil {
		log.Fatalf("subscribe to quota: %v", err)
	}

	// Report 1 500 requests → ceil(1500/1000) * $2 = $4.
	if _, err := client.ReportUsage(ctx, customerID, "api_request", 1500); err != nil {
		log.Fatalf("report usage: %v", err)
	}

	// Prepaid credit grant: include 1 000 requests for free each month. Re-grant
	// from an invoice.paid webhook to renew each billing cycle.
	grant, err := client.GrantQuota(ctx, stripe.QuotaGrantParams{
		Customer:  customerID,
		Amount:    stripe.Dollars(2),
		ExpiresIn: 30 * 24 * time.Hour,
		Name:      "Monthly included requests",
		PriceIDs:  []string{quota.Price.ID},
	})
	if err != nil {
		log.Fatalf("grant quota: %v", err)
	}
	fmt.Println("credit grant:", grant.ID)

	// ── One-off purchase via Checkout ─────────────────────────────────────────
	//
	// Build a cart and open a hosted Checkout page. Redirect the buyer to
	// session.URL; Stripe handles payment capture and redirects back.
	cart := stripe.NewCart("usd").
		AddItem("Pro T-Shirt", stripe.Dollars(35), 1).
		AddItem("Sticker pack", stripe.Dollars(5), 3).
		WithAutomaticTax()

	session, err := client.Checkout(ctx, stripe.CheckoutParams{
		Cart:       cart,
		SuccessURL: "https://example.com/success?session_id={CHECKOUT_SESSION_ID}",
		CancelURL:  "https://example.com/cancel",
	})
	if err != nil {
		log.Fatalf("checkout: %v", err)
	}
	fmt.Println("checkout URL:", session.URL)

	// ── Refund ────────────────────────────────────────────────────────────────
	//
	// Full refund — pass Amount 0 (or omit) to refund the entire captured amount.
	// Partial refund — set Amount to the minor-unit value to return.
	if piID := os.Getenv("STRIPE_PAYMENT_INTENT_ID"); piID != "" {
		// Full refund.
		refund, err := client.RefundPayment(ctx, stripe.RefundParams{
			PaymentIntentID: piID,
			Reason:          stripe.RefundRequestedByCustomer,
		})
		if err != nil {
			log.Fatalf("refund: %v", err)
		}
		fmt.Printf("refund: %s  amount: %d\n", refund.ID, refund.Amount)

		// Partial refund ($5.00 back).
		partial, err := client.RefundPayment(ctx, stripe.RefundParams{
			PaymentIntentID: piID,
			Amount:          stripe.Dollars(5),
			Reason:          stripe.RefundRequestedByCustomer,
		})
		if err != nil {
			log.Fatalf("partial refund: %v", err)
		}
		fmt.Printf("partial refund: %s  amount: %d\n", partial.ID, partial.Amount)
	}

	// ── Subscription cancellation (with prorated refund) ──────────────────────
	//
	// UnsubscribeWithRefund cancels immediately and refunds the unused portion
	// of the current billing period.
	cancelled, refund, err := client.UnsubscribeWithRefund(ctx, sub.ID)
	if err != nil {
		log.Fatalf("cancel with refund: %v", err)
	}
	fmt.Printf("subscription %s cancelled, refund %s issued\n", cancelled.Status, refund.ID)

	// Void any remaining prepaid quota so it cannot be consumed after cancellation.
	if _, err := client.VoidCreditGrant(ctx, grant.ID); err != nil {
		log.Fatalf("void grant: %v", err)
	}
	fmt.Println("credit grant voided")
}
