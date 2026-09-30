// Guide 07: manage subscriptions from your server — upgrade, preview the next
// invoice, discount, cancel, resume and refund. See README.md.
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

	// 1. A customer with a saved test card, so subscriptions bill immediately.
	cus, err := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
		Email:         "manage@example.com",
		PaymentMethod: "pm_card_visa",
	})
	if err != nil {
		fatal("create customer", "err", err)
	}
	slog.Info("1. customer with card", "customer", cus.ID)

	// 2. Plans (safe to re-run thanks to lookup keys).
	basic, err := client.EnsurePlan(ctx, stripe.PlanParams{
		LookupKey: "guide_basic_monthly", ProductName: "Basic",
		Amount: stripe.Dollars(10), Interval: stripe.Monthly,
	})
	if err != nil {
		fatal("ensure basic plan", "err", err)
	}
	pro, err := client.EnsurePlan(ctx, stripe.PlanParams{
		LookupKey: "guide_pro_monthly", ProductName: "Pro",
		Amount: stripe.Dollars(20), Interval: stripe.Monthly,
	})
	if err != nil {
		fatal("ensure pro plan", "err", err)
	}
	proYearly, err := client.EnsurePlan(ctx, stripe.PlanParams{
		LookupKey: "guide_pro_yearly", ProductID: pro.Product.ID,
		Amount: stripe.Dollars(200), Interval: stripe.Yearly,
	})
	if err != nil {
		fatal("ensure pro yearly plan", "err", err)
	}
	slog.Info("2. plans", "basic", basic.ID, "pro", pro.ID, "pro_yearly", proYearly.ID)

	// 3. Subscribe to Basic. The saved card is charged right away.
	sub, err := client.Subscribe(ctx, stripe.SubscribeParams{Customer: cus.ID, PriceID: basic.ID})
	if err != nil {
		fatal("subscribe", "err", err)
	}
	slog.Info("3. subscribed to basic", "subscription", sub.ID, "status", sub.Status)

	// 4. What will the next invoice be?
	next, err := client.UpcomingInvoice(ctx, sub.ID)
	if err != nil {
		fatal("upcoming invoice", "err", err)
	}
	slog.Info("4. next invoice", "amount_due", money(next.AmountDue))

	// 5. Upgrade Basic → Pro. Stripe prorates: the unused Basic time is
	//    credited and the Pro difference lands on the next invoice.
	sub, err = client.SwapPlan(ctx, sub.ID, pro.ID)
	if err != nil {
		fatal("swap plan", "err", err)
	}
	next, err = client.UpcomingInvoice(ctx, sub.ID)
	if err != nil {
		fatal("upcoming invoice", "err", err)
	}
	slog.Info("5. upgraded to pro", "subscription", sub.ID, "next_invoice", money(next.AmountDue))

	// 6. A 20% coupon, applied to a second (yearly) subscription.
	coupon, err := client.CreateCoupon(ctx, stripe.CouponParams{
		Name: "Yearly 20% off", PercentOff: 20, Duration: stripe.CouponOnce,
	})
	if err != nil {
		fatal("create coupon", "err", err)
	}
	yearlySub, err := client.Subscribe(ctx, stripe.SubscribeParams{
		Customer: cus.ID, PriceID: proYearly.ID, CouponID: coupon.ID,
	})
	if err != nil {
		fatal("subscribe yearly with coupon", "err", err)
	}
	slog.Info("6. yearly with coupon", "subscription", yearlySub.ID, "coupon", coupon.ID)

	// 7. Cancel at period end: access continues until the paid period is over.
	sub, err = client.Unsubscribe(ctx, sub.ID, stripe.CancelAtPeriodEnd)
	if err != nil {
		fatal("cancel at period end", "err", err)
	}
	slog.Info("7. cancels at period end", "status", sub.Status, "has_access", stripe.HasAccess(sub),
		"access_until", stripe.SubscriptionAccessUntil(sub).Format(time.DateOnly))

	// 8. Changed their mind before the period ended.
	sub, err = client.Resubscribe(ctx, sub.ID)
	if err != nil {
		fatal("resubscribe", "err", err)
	}
	slog.Info("8. resumed", "status", sub.Status, "cancel_at_period_end", sub.CancelAtPeriodEnd)

	// 9. Money-back guarantee: cancel now and refund the last payment in full.
	yearlySub, refund, err := client.UnsubscribeWithRefund(ctx, yearlySub.ID)
	if err != nil {
		fatal("cancel with refund", "err", err)
	}
	if refund != nil {
		slog.Info("9. canceled with refund", "status", yearlySub.Status, "refund", refund.ID, "amount", money(refund.Amount))
	} else {
		slog.Info("9. canceled, nothing to refund", "status", yearlySub.Status)
	}

	// 10. Cancel immediately, no refund.
	sub, err = client.Unsubscribe(ctx, sub.ID, stripe.CancelImmediately)
	if err != nil {
		fatal("cancel now", "err", err)
	}
	slog.Info("10. canceled now", "status", sub.Status, "has_access", stripe.HasAccess(sub))

	// 11. Everything this customer has (canceled subscriptions are included).
	subs, err := client.ListSubscriptions(ctx, cus.ID)
	if err != nil {
		fatal("list subscriptions", "err", err)
	}
	for _, s := range subs {
		slog.Info("11. subscription", "id", s.ID, "status", s.Status)
	}

	// 12. Retire the coupon. Discounts already applied are not affected.
	if err := client.DeleteCoupon(ctx, coupon.ID); err != nil {
		fatal("delete coupon", "err", err)
	}
	slog.Info("12. coupon deleted", "coupon", coupon.ID)
	slog.Info("✅ done — see the customer in the Dashboard", "url", "https://dashboard.stripe.com/test/customers/"+cus.ID)
}

// money formats minor units (cents) for logging.
func money(cents int64) string { return fmt.Sprintf("$%.2f", float64(cents)/100) }

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
