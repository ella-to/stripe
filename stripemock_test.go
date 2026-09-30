package stripe

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestAgainstStripeMock sends every wrapper request to stripe-mock, which
// validates parameters against Stripe's OpenAPI spec. It does not check
// business logic (stripe-mock is stateless), only that requests are well
// formed. Run it with:
//
//	go install github.com/stripe/stripe-mock@latest && stripe-mock &
//	STRIPE_MOCK_URL=http://localhost:12111 go test -run StripeMock ./...
func TestAgainstStripeMock(t *testing.T) {
	url := os.Getenv("STRIPE_MOCK_URL")
	if url == "" {
		t.Skip("set STRIPE_MOCK_URL (e.g. http://localhost:12111) to run against stripe-mock")
	}
	ctx := context.Background()
	c := newTestClient(url)
	c.feeResolver = FeeResolverFunc(func(context.Context, string) (PlatformFee, bool, error) {
		return PlatformFee{Percent: 5, Fixed: 30}, true, nil
	})

	check := func(name string, err error) {
		t.Helper()
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	var err error

	// Customers
	_, err = c.CreateCustomer(ctx, CreateCustomerParams{Email: "a@example.test", Name: "A", Phone: "1", Description: "d", Metadata: map[string]string{"k": "v"}, PaymentMethod: "pm_card_visa"})
	check("CreateCustomer", err)
	_, err = c.GetCustomer(ctx, "cus_1")
	check("GetCustomer", err)
	_, err = c.UpdateCustomer(ctx, "cus_1", UpdateCustomerParams{Email: "b@example.test", Name: "B", Metadata: map[string]string{"k": "v"}})
	check("UpdateCustomer", err)
	_, _, err = c.FindCustomerByEmail(ctx, "a@example.test")
	check("FindCustomerByEmail", err)
	_, err = c.AttachPaymentMethod(ctx, "cus_1", "pm_card_visa")
	check("AttachPaymentMethod", err)
	check("DeleteCustomer", c.DeleteCustomer(ctx, "cus_1"))

	// Payments
	_, err = c.ChargeCustomer(ctx, ChargeCustomerParams{Customer: "cus_1", Amount: 500, PaymentMethod: "pm_1", Description: "d", ReceiptEmail: "a@example.test", Metadata: map[string]string{"k": "v"}, IdempotencyKey: "k1"})
	check("ChargeCustomer", err)
	_, err = c.ChargeWithFee(ctx, ChargeParams{ConnectedAccount: "acct_1", Amount: 1000, Customer: "cus_1", PaymentMethod: "pm_1", Confirm: true, Description: "d"})
	check("ChargeWithFee", err)
	_, err = c.RefundPayment(ctx, RefundParams{PaymentIntentID: "pi_1", Amount: 100, Reason: RefundRequestedByCustomer, ConnectedAccount: "acct_1", RefundApplicationFee: true})
	check("RefundPayment", err)

	// Checkout
	cart := NewCart("usd").AddItem("Shirt", 2500, 2).AddPrice("price_1", 1).
		Add(CartItem{Name: "Book", Description: "d", Amount: 1000, Images: []string{"https://example.test/i.png"}, TaxRateIDs: []string{"txr_1"}}).
		AddShipping("Standard", 500).ShipTo("US", "CA").WithTaxRates("txr_2")
	_, err = c.Checkout(ctx, CheckoutParams{Cart: cart, SuccessURL: "https://example.test/ok?session_id={CHECKOUT_SESSION_ID}", CancelURL: "https://example.test/no", CustomerEmail: "a@example.test", ClientReferenceID: "o1", AllowPromotionCodes: true, Metadata: map[string]string{"k": "v"}})
	check("Checkout", err)
	_, err = c.Checkout(ctx, CheckoutParams{Cart: NewCart("usd").AddItem("x", 100, 1).WithAutomaticTax().ShipTo("US"), SuccessURL: "https://example.test/ok", CancelURL: "https://example.test/no", Customer: "cus_1", CouponID: "co_1", ConnectedAccount: "acct_1"})
	check("Checkout (tax, connect)", err)
	_, err = c.GetCheckoutSession(ctx, "cs_1")
	check("GetCheckoutSession", err)
	_, err = c.CheckoutSubscription(ctx, SubscriptionCheckoutParams{PriceID: "price_1", Quantity: 2, MeteredPriceIDs: []string{"price_2"}, SuccessURL: "https://example.test/ok", CancelURL: "https://example.test/no", CustomerEmail: "a@example.test", ClientReferenceID: "u1", TrialDays: 7, TrialWithoutCard: true, AllowPromotionCodes: true, AutomaticTax: true, Metadata: map[string]string{"k": "v"}})
	check("CheckoutSubscription", err)
	_, err = c.CheckoutSubscription(ctx, SubscriptionCheckoutParams{PriceID: "price_1", SuccessURL: "https://example.test/ok", CancelURL: "https://example.test/no", Customer: "cus_1", CouponID: "co_1", ConnectedAccount: "acct_1"})
	check("CheckoutSubscription (connect)", err)

	// Portal
	_, err = c.CustomerPortal(ctx, PortalParams{Customer: "cus_1", ReturnURL: "https://example.test", ConfigurationID: "bpc_1"})
	check("CustomerPortal", err)
	_, err = c.CreatePortalConfiguration(ctx, PortalConfigParams{Headline: "h", ReturnURL: "https://example.test", AllowUpdatePaymentMethod: true, AllowInvoiceHistory: true, AllowUpdateDetails: true, AllowCancel: true, SwitchPrices: []string{"price_1", "price_2"}})
	check("CreatePortalConfiguration", err)
	_, err = c.CreatePortalConfiguration(ctx, PortalConfigParams{AllowCancel: true, CancelImmediately: true})
	check("CreatePortalConfiguration (minimal)", err)

	// Subscriptions
	_, err = c.CreatePlan(ctx, PlanParams{ProductName: "Pro", LookupKey: "pro_monthly", Amount: 2000, Interval: Monthly, IntervalCount: 1, TrialDays: 7, Metadata: map[string]string{"k": "v"}})
	check("CreatePlan", err)
	_, err = c.CreatePlan(ctx, PlanParams{ProductID: "prod_1", Amount: 20000, Interval: Yearly})
	check("CreatePlan (product)", err)
	_, _, err = c.FindPrice(ctx, "pro_monthly")
	check("FindPrice", err)
	_, err = c.EnsurePlan(ctx, PlanParams{ProductName: "Pro", LookupKey: "pro_monthly", Amount: 2000, Interval: Monthly})
	check("EnsurePlan", err)
	_, err = c.Subscribe(ctx, SubscribeParams{Customer: "cus_1", PriceID: "price_1", Quantity: 2, TrialDays: 7, CouponID: "co_1", Metadata: map[string]string{"k": "v"}})
	check("Subscribe", err)
	_, err = c.Subscribe(ctx, SubscribeParams{Customer: "cus_1", PriceID: "price_1", TrialEnd: time.Now().AddDate(0, 1, 0), ConnectedAccount: "acct_1"})
	check("Subscribe (connect)", err)
	_, err = c.GetSubscription(ctx, "sub_1")
	check("GetSubscription", err)
	_, err = c.SwapPlan(ctx, "sub_1", "price_2")
	check("SwapPlan", err)
	_, err = c.Unsubscribe(ctx, "sub_1", CancelAtPeriodEnd)
	check("Unsubscribe (period end)", err)
	_, err = c.Unsubscribe(ctx, "sub_1", CancelImmediately)
	check("Unsubscribe (now)", err)
	_, err = c.Resubscribe(ctx, "sub_1")
	check("Resubscribe", err)
	_, _, err = c.UnsubscribeWithRefund(ctx, "sub_1")
	check("UnsubscribeWithRefund", err)
	_, err = c.ListSubscriptions(ctx, "cus_1")
	check("ListSubscriptions", err)
	_, err = c.UpcomingInvoice(ctx, "sub_1")
	check("UpcomingInvoice", err)
	_, err = c.CreateCoupon(ctx, CouponParams{Name: "20", PercentOff: 20, Duration: CouponRepeating, DurationMonths: 3, MaxRedemptions: 10})
	check("CreateCoupon (percent)", err)
	_, err = c.CreateCoupon(ctx, CouponParams{ID: "FIVE", AmountOff: 500, Currency: "usd", Duration: CouponForever})
	check("CreateCoupon (amount)", err)
	check("DeleteCoupon", c.DeleteCoupon(ctx, "co_1"))

	// Usage based billing
	_, err = c.CreateMeter(ctx, MeterParams{DisplayName: "API", EventName: "api_request", Aggregation: AggCount})
	check("CreateMeter", err)
	_, _, err = c.FindMeter(ctx, "api_request")
	check("FindMeter", err)
	_, err = c.CreateMeteredPrice(ctx, MeteredPriceParams{ProductName: "API", MeterID: "mtr_1", AmountPerPackage: 200, PackageSize: 1000, LookupKey: "api"})
	check("CreateMeteredPrice", err)
	_, err = c.SetupMeteredQuota(ctx, SetupMeteredQuotaParams{ProductName: "API", EventName: "api_request", AmountPerPackage: 200, PackageSize: 1000})
	check("SetupMeteredQuota", err)
	_, err = c.ReportUsage(ctx, "cus_1", "api_request", 10)
	check("ReportUsage", err)
	_, err = c.ReportUsageEvent(ctx, UsageEvent{Customer: "cus_1", EventName: "api_request", Value: 1, Identifier: "req_1", Timestamp: time.Now()})
	check("ReportUsageEvent", err)
	_, err = c.GrantQuota(ctx, QuotaGrantParams{Customer: "cus_1", Amount: 200, ExpiresIn: time.Hour, Name: "n", PriceIDs: []string{"price_1"}, Metadata: map[string]string{"k": "v"}})
	check("GrantQuota (prices)", err)
	_, err = c.GrantQuota(ctx, QuotaGrantParams{Customer: "cus_1", Amount: 200})
	check("GrantQuota (metered)", err)
	_, err = c.VoidCreditGrant(ctx, "credgr_1")
	check("VoidCreditGrant", err)

	// Tax
	_, err = c.CreateTaxRate(ctx, TaxRateParams{DisplayName: "VAT", Percentage: 19, Inclusive: true, Country: "DE", State: "BE", Jurisdiction: "DE", Description: "d", TaxType: "vat", Metadata: map[string]string{"k": "v"}})
	check("CreateTaxRate", err)
	_, err = c.CreateTaxRateForAccount(ctx, "acct_1", TaxRateParams{DisplayName: "GST", Percentage: 5})
	check("CreateTaxRateForAccount", err)
	_, err = c.ListTaxRates(ctx, true)
	check("ListTaxRates", err)
	_, err = c.DeactivateTaxRate(ctx, "txr_1")
	check("DeactivateTaxRate", err)

	// Connect
	_, err = c.RegisterConnectedAccount(ctx, ConnectedAccountParams{Type: AccountExpress, Email: "s@example.test", Country: "US", BusinessType: "individual", Capabilities: []string{"card_payments", "transfers"}, Metadata: map[string]string{"k": "v"}})
	check("RegisterConnectedAccount", err)
	_, err = c.GetConnectedAccount(ctx, "acct_1")
	check("GetConnectedAccount", err)
	_, err = c.UpdateConnectedAccount(ctx, "acct_1", ConnectedAccountUpdate{Email: "t@example.test", Metadata: map[string]string{"k": "v"}, Defaults: map[string]string{"business_profile[url]": "https://example.test"}})
	check("UpdateConnectedAccount", err)
	_, err = c.AccountOnboardingLink(ctx, "acct_1", "https://example.test/r", "https://example.test/d")
	check("AccountOnboardingLink", err)
	_, err = c.RequestAccountUpdate(ctx, "acct_1", "https://example.test/r", "https://example.test/d")
	check("RequestAccountUpdate", err)
	_, err = c.ExpressDashboardLink(ctx, "acct_1")
	check("ExpressDashboardLink", err)
	_, err = c.SetPlatformFee(ctx, "acct_1", PlatformFee{Percent: 2.9, Fixed: 30})
	check("SetPlatformFee", err)
	check("DeleteConnectedAccount", c.DeleteConnectedAccount(ctx, "acct_1"))

	// Webhook endpoints
	_, err = c.CreateWebhookEndpoint(ctx, WebhookEndpointParams{URL: "https://example.test/webhook", Events: []string{"invoice.paid"}, Description: "d", Connect: true, Metadata: map[string]string{"k": "v"}})
	check("CreateWebhookEndpoint", err)
	_, err = c.UpdateWebhookEndpoint(ctx, "we_1", WebhookEndpointParams{URL: "https://example.test/webhook2", Events: []string{"*"}})
	check("UpdateWebhookEndpoint", err)
	check("DeleteWebhookEndpoint", c.DeleteWebhookEndpoint(ctx, "we_1"))
}
