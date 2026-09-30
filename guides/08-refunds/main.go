// Guide 08: charge a saved card, then refund it in parts. See README.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"ella.to/stripe"
)

func main() {
	client := stripe.New(mustEnv("STRIPE_SECRET_KEY"))
	ctx := context.Background()

	// Refund a Checkout purchase instead: go run ./guides/08-refunds cs_test_...
	if len(os.Args) > 1 {
		refundCheckout(ctx, client, os.Args[1])
		return
	}

	// 1. A customer with a saved test card.
	cus, err := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
		Email:         "refunds@example.com",
		PaymentMethod: "pm_card_visa",
	})
	if err != nil {
		fatal("create customer", "err", err)
	}
	slog.Info("created customer", "id", cus.ID)

	// 2. Charge $50. The idempotency key makes a retry return the same payment.
	orderID := fmt.Sprintf("order-%d", time.Now().UnixNano())
	pi, err := client.ChargeCustomer(ctx, stripe.ChargeCustomerParams{
		Customer:       cus.ID,
		Amount:         stripe.Dollars(50),
		Description:    "Order " + orderID,
		Metadata:       map[string]string{"order_id": orderID},
		IdempotencyKey: orderID,
	})
	if err != nil {
		fatal("charge", "err", err)
	}
	slog.Info("charged", "payment_intent", pi.ID, "amount", pi.Amount, "status", pi.Status)

	// 3. Partial refund: $20 back.
	partial, err := client.RefundPayment(ctx, stripe.RefundParams{
		PaymentIntentID: pi.ID,
		Amount:          stripe.Dollars(20),
		Reason:          stripe.RefundRequestedByCustomer,
	})
	if err != nil {
		fatal("partial refund", "err", err)
	}
	slog.Info("partial refund", "id", partial.ID, "amount", partial.Amount, "status", partial.Status)

	// 4. Refund whatever is left (Amount 0 = the remaining balance).
	rest, err := client.RefundPayment(ctx, stripe.RefundParams{
		PaymentIntentID: pi.ID,
		Reason:          stripe.RefundRequestedByCustomer,
	})
	if err != nil {
		fatal("refund the rest", "err", err)
	}
	slog.Info("refunded the rest", "id", rest.ID, "amount", rest.Amount, "status", rest.Status)
}

// refundCheckout fully refunds the payment behind a completed Checkout session.
func refundCheckout(ctx context.Context, client *stripe.Client, sessionID string) {
	cs, err := client.GetCheckoutSession(ctx, sessionID)
	if err != nil {
		fatal("get checkout session", "err", err)
	}
	if cs.PaymentIntent == nil {
		fatal("session has no payment (not paid yet?)", "session", cs.ID)
	}
	refund, err := client.RefundPayment(ctx, stripe.RefundParams{
		PaymentIntentID: cs.PaymentIntent.ID,
		Reason:          stripe.RefundRequestedByCustomer,
	})
	if err != nil {
		fatal("refund", "err", err)
	}
	slog.Info("refunded checkout", "session", cs.ID, "refund", refund.ID, "amount", refund.Amount, "status", refund.Status)
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
