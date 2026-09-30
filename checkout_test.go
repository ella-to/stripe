package stripe

import (
	"context"
	"testing"
)

func TestCheckoutCustomerAndTax(t *testing.T) {
	client, cs := newCaptureClient(t, `{"id":"cs_1","object":"checkout.session"}`)
	cart := NewCart("usd").
		AddItem("T-Shirt", Dollars(25), 2).
		WithAutomaticTax().
		ShipTo("US", "CA").
		AddShipping("Standard", Dollars(5))

	_, err := client.Checkout(context.Background(), CheckoutParams{
		Cart:                cart,
		SuccessURL:          "https://example.test/ok",
		CancelURL:           "https://example.test/cancel",
		Customer:            "cus_1",
		CustomerEmail:       "ignored@example.test",
		ClientReferenceID:   "order-1",
		AllowPromotionCodes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	f := cs.last(t).Form
	wantForm(t, f, "mode", "payment")
	wantForm(t, f, "customer", "cus_1")
	wantNoForm(t, f, "customer_email")
	wantForm(t, f, "customer_update[address]", "auto")
	wantForm(t, f, "customer_update[shipping]", "auto")
	wantForm(t, f, "shipping_address_collection[allowed_countries][0]", "US")
	wantForm(t, f, "shipping_address_collection[allowed_countries][1]", "CA")
	wantForm(t, f, "client_reference_id", "order-1")
	wantForm(t, f, "allow_promotion_codes", "true")
	wantForm(t, f, "automatic_tax[enabled]", "true")
	wantForm(t, f, "line_items[0][price_data][unit_amount]", "2500")
	wantForm(t, f, "line_items[0][quantity]", "2")
}

func TestCheckoutCartTaxRates(t *testing.T) {
	client, cs := newCaptureClient(t, `{"id":"cs_1","object":"checkout.session"}`)
	cart := NewCart("usd").
		AddItem("Mug", Dollars(10), 1).
		Add(CartItem{Name: "Book", Amount: Dollars(20), TaxRateIDs: []string{"txr_book"}}).
		WithTaxRates("txr_default")

	if _, err := client.Checkout(context.Background(), CheckoutParams{
		Cart: cart, SuccessURL: "https://x.test/ok", CancelURL: "https://x.test/no",
		CustomerEmail: "buyer@example.test",
	}); err != nil {
		t.Fatal(err)
	}
	f := cs.last(t).Form
	wantForm(t, f, "customer_email", "buyer@example.test")
	wantForm(t, f, "line_items[0][tax_rates][0]", "txr_default")
	wantForm(t, f, "line_items[1][tax_rates][0]", "txr_book")
	wantNoForm(t, f, "customer_update[address]")
}

func TestCheckoutRejectsCouponAndPromotionCodes(t *testing.T) {
	client, _ := newCaptureClient(t, `{}`)
	_, err := client.Checkout(context.Background(), CheckoutParams{
		Cart: NewCart("").AddItem("x", 100, 1), SuccessURL: "a", CancelURL: "b",
		CouponID: "c", AllowPromotionCodes: true,
	})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestCheckoutSubscription(t *testing.T) {
	client, cs := newCaptureClient(t, `{"id":"cs_1","object":"checkout.session"}`)
	_, err := client.CheckoutSubscription(context.Background(), SubscriptionCheckoutParams{
		PriceID:          "price_base",
		MeteredPriceIDs:  []string{"price_usage"},
		SuccessURL:       "https://x.test/ok",
		CancelURL:        "https://x.test/no",
		CustomerEmail:    "new@example.test",
		TrialDays:        14,
		TrialWithoutCard: true,
		Metadata:         map[string]string{"user_id": "u1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := cs.last(t).Form
	wantForm(t, f, "mode", "subscription")
	wantForm(t, f, "line_items[0][price]", "price_base")
	wantForm(t, f, "line_items[0][quantity]", "1")
	wantForm(t, f, "line_items[1][price]", "price_usage")
	wantNoForm(t, f, "line_items[1][quantity]")
	wantForm(t, f, "subscription_data[trial_period_days]", "14")
	wantForm(t, f, "payment_method_collection", "if_required")
	wantForm(t, f, "subscription_data[trial_settings][end_behavior][missing_payment_method]", "cancel")
	wantForm(t, f, "metadata[user_id]", "u1")
	wantForm(t, f, "subscription_data[metadata][user_id]", "u1")
}

func TestCheckoutSubscriptionConnectedFee(t *testing.T) {
	client, cs := newCaptureClient(t, `{"id":"cs_1","object":"checkout.session"}`)
	client.feeResolver = FeeResolverFunc(func(context.Context, string) (PlatformFee, bool, error) {
		return PlatformFee{Percent: 7.5}, true, nil
	})
	if _, err := client.CheckoutSubscription(context.Background(), SubscriptionCheckoutParams{
		PriceID: "price_1", SuccessURL: "a", CancelURL: "b", ConnectedAccount: "acct_1",
	}); err != nil {
		t.Fatal(err)
	}
	wantForm(t, cs.last(t).Form, "subscription_data[application_fee_percent]", "7.5000")
}

func TestRefundOnConnectedAccount(t *testing.T) {
	client, cs := newCaptureClient(t, `{"id":"re_1","object":"refund"}`)
	if _, err := client.RefundPayment(context.Background(), RefundParams{
		PaymentIntentID: "pi_1", Amount: 500, ConnectedAccount: "acct_1", RefundApplicationFee: true,
	}); err != nil {
		t.Fatal(err)
	}
	f := cs.last(t).Form
	wantForm(t, f, "payment_intent", "pi_1")
	wantForm(t, f, "amount", "500")
	wantForm(t, f, "refund_application_fee", "true")
}
