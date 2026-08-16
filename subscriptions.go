package stripe

import (
	"context"
	"fmt"
	"time"

	sgo "github.com/stripe/stripe-go/v86"
)

// Interval is the billing cadence of a recurring plan.
type Interval string

const (
	// Monthly bills once per month.
	Monthly Interval = "month"
	// Yearly bills once per year.
	Yearly Interval = "year"
	// Weekly bills once per week.
	Weekly Interval = "week"
	// Daily bills once per day.
	Daily Interval = "day"
)

// PlanParams describes a recurring plan. CreatePlan turns it into a Stripe
// Product + recurring Price in a single call.
type PlanParams struct {
	ProductName   string
	Amount        int64    // Price per interval in minor units (use Dollars).
	Currency      string   // Defaults to "usd".
	Interval      Interval // Monthly or Yearly, etc.
	IntervalCount int64    // Defaults to 1 (e.g. set 3 with Monthly for quarterly).
	TrialDays     int64    // Optional default trial baked into the price.
	Metadata      map[string]string
}

// CreatePlan creates a recurring price (with its product) you can subscribe
// customers to. The returned Price.ID is what you pass to Subscribe.
func (c *Client) CreatePlan(ctx context.Context, p PlanParams) (*Price, error) {
	if p.Interval == "" {
		return nil, fmt.Errorf("stripe: CreatePlan requires an Interval (Monthly/Yearly)")
	}
	if p.Currency == "" {
		p.Currency = "usd"
	}
	if p.IntervalCount == 0 {
		p.IntervalCount = 1
	}

	recurring := &sgo.PriceCreateRecurringParams{
		Interval:      String(string(p.Interval)),
		IntervalCount: Int64(p.IntervalCount),
	}
	if p.TrialDays > 0 {
		recurring.TrialPeriodDays = Int64(p.TrialDays)
	}

	params := &sgo.PriceCreateParams{
		Currency:   String(p.Currency),
		UnitAmount: Int64(p.Amount),
		Recurring:  recurring,
		ProductData: &sgo.PriceCreateProductDataParams{
			Name: String(p.ProductName),
		},
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	c.prep(&params.Params)
	return c.api.V1Prices.Create(ctx, params)
}

// SubscribeParams describes a new subscription for an existing customer.
type SubscribeParams struct {
	Customer string // Customer id ("cus_...").
	PriceID  string // Recurring price id ("price_..."), e.g. from CreatePlan.
	Quantity int64  // Defaults to 1.

	// Trial controls (optional, mutually exclusive). TrialDays is the simplest
	// way to express "N days". For "N months" pass TrialEnd, e.g.
	// time.Now().AddDate(0, 3, 0) for a 3 month trial.
	TrialDays int64
	TrialEnd  time.Time

	// CouponID applies a discount, if any.
	CouponID string
	Metadata map[string]string

	// ConnectedAccount, when set, creates the subscription on behalf of that
	// connected account ("acct_..."), making it the settlement merchant.
	ConnectedAccount string

	// FeePercent is the recurring platform fee, as a percentage of each
	// invoice, transferred to your platform (Stripe's application_fee_percent).
	// Stripe only supports a percentage fee on subscriptions; for a fixed
	// per-invoice fee, add an invoice item or use metered overage instead.
	// When zero and ConnectedAccount is set, the percentage stored for the
	// account via SetPlatformFee is used.
	FeePercent float64
}

// Subscribe creates a subscription. For monthly vs yearly, pass the matching
// PriceID created by CreatePlan.
func (c *Client) Subscribe(ctx context.Context, p SubscribeParams) (*Subscription, error) {
	if p.Customer == "" || p.PriceID == "" {
		return nil, fmt.Errorf("stripe: Subscribe requires Customer and PriceID")
	}
	qty := p.Quantity
	if qty == 0 {
		qty = 1
	}

	params := &sgo.SubscriptionCreateParams{
		Customer: String(p.Customer),
		Items: []*sgo.SubscriptionCreateItemParams{
			{Price: String(p.PriceID), Quantity: Int64(qty)},
		},
	}
	switch {
	case !p.TrialEnd.IsZero():
		params.TrialEnd = Int64(p.TrialEnd.Unix())
	case p.TrialDays > 0:
		params.TrialPeriodDays = Int64(p.TrialDays)
	}
	if p.CouponID != "" {
		params.Discounts = []*sgo.SubscriptionCreateDiscountParams{
			{Coupon: String(p.CouponID)},
		}
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}

	eff := c
	if p.ConnectedAccount != "" {
		eff = c.ForAccount(p.ConnectedAccount)

		feePercent := p.FeePercent
		if feePercent == 0 {
			fee, ok, err := c.GetPlatformFee(ctx, p.ConnectedAccount)
			if err != nil {
				return nil, err
			}
			if ok {
				feePercent = fee.Percent
			}
		}
		if feePercent > 0 {
			params.ApplicationFeePercent = Float64(feePercent)
		}
	}

	eff.prep(&params.Params)
	return eff.api.V1Subscriptions.Create(ctx, params)
}

// CancelMode controls how a subscription is terminated.
type CancelMode int

const (
	// CancelImmediately ends the subscription right now.
	CancelImmediately CancelMode = iota
	// CancelAtPeriodEnd lets the customer keep access until the paid period
	// they already have expires, then cancels.
	CancelAtPeriodEnd
)

// Unsubscribe cancels a subscription either immediately or at period end.
func (c *Client) Unsubscribe(ctx context.Context, subscriptionID string, mode CancelMode) (*Subscription, error) {
	if mode == CancelAtPeriodEnd {
		params := &sgo.SubscriptionUpdateParams{
			CancelAtPeriodEnd: Bool(true),
		}
		c.prep(&params.Params)
		return c.api.V1Subscriptions.Update(ctx, subscriptionID, params)
	}

	params := &sgo.SubscriptionCancelParams{}
	c.prep(&params.Params)
	return c.api.V1Subscriptions.Cancel(ctx, subscriptionID, params)
}

// GetSubscription retrieves a subscription by id.
func (c *Client) GetSubscription(ctx context.Context, subscriptionID string) (*Subscription, error) {
	params := &sgo.SubscriptionRetrieveParams{}
	c.prep(&params.Params)
	return c.api.V1Subscriptions.Retrieve(ctx, subscriptionID, params)
}

// SwapPlan moves an existing subscription onto a new price (e.g. upgrading
// monthly -> yearly). Proration is left at Stripe's default behaviour.
func (c *Client) SwapPlan(ctx context.Context, subscriptionID, newPriceID string) (*Subscription, error) {
	sub, err := c.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if sub.Items == nil || len(sub.Items.Data) == 0 {
		return nil, fmt.Errorf("stripe: subscription %s has no items to swap", subscriptionID)
	}

	params := &sgo.SubscriptionUpdateParams{
		Items: []*sgo.SubscriptionUpdateItemParams{
			{
				ID:    String(sub.Items.Data[0].ID),
				Price: String(newPriceID),
			},
		},
	}
	c.prep(&params.Params)
	return c.api.V1Subscriptions.Update(ctx, subscriptionID, params)
}

// Resubscribe re-activates a subscription that was scheduled to cancel at
// period end (CancelAtPeriodEnd == true). For a subscription that is already
// paused/past_due, it resumes billing from today.
//
// If the subscription is fully cancelled (status == "canceled"), create a new
// one via Subscribe instead.
func (c *Client) Resubscribe(ctx context.Context, subscriptionID string) (*Subscription, error) {
	sub, err := c.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	// Pending cancel-at-period-end: just clear the flag.
	if sub.CancelAtPeriodEnd {
		params := &sgo.SubscriptionUpdateParams{CancelAtPeriodEnd: Bool(false)}
		c.prep(&params.Params)
		return c.api.V1Subscriptions.Update(ctx, subscriptionID, params)
	}
	// Paused subscription: resume it.
	if sub.Status == sgo.SubscriptionStatusPaused {
		params := &sgo.SubscriptionResumeParams{}
		c.prep(&params.Params)
		return c.api.V1Subscriptions.Resume(ctx, subscriptionID, params)
	}
	return sub, nil
}

// UnsubscribeWithRefund cancels the subscription immediately and issues a full
// refund of the most recent invoice's payment. Use this for "cancel and refund"
// flows (e.g. within a money-back guarantee window).
//
// The returned Refund is nil when the latest invoice has no captured payment
// (e.g. a free trial that never billed).
func (c *Client) UnsubscribeWithRefund(ctx context.Context, subscriptionID string) (*Subscription, *Refund, error) {
	// Fetch with payments expanded so we can find the payment intent to refund.
	getParams := &sgo.SubscriptionRetrieveParams{}
	getParams.AddExpand("latest_invoice.payments")
	c.prep(&getParams.Params)
	sub, err := c.api.V1Subscriptions.Retrieve(ctx, subscriptionID, getParams)
	if err != nil {
		return nil, nil, err
	}

	// Locate the default payment intent on the latest invoice.
	var piID string
	if sub.LatestInvoice != nil && sub.LatestInvoice.Payments != nil {
		for _, ip := range sub.LatestInvoice.Payments.Data {
			if ip.IsDefault && ip.Payment != nil && ip.Payment.PaymentIntent != nil {
				piID = ip.Payment.PaymentIntent.ID
				break
			}
		}
	}

	cancelParams := &sgo.SubscriptionCancelParams{}
	c.prep(&cancelParams.Params)
	cancelled, err := c.api.V1Subscriptions.Cancel(ctx, subscriptionID, cancelParams)
	if err != nil {
		return nil, nil, err
	}

	if piID == "" {
		return cancelled, nil, nil
	}
	refund, err := c.RefundPayment(ctx, RefundParams{
		PaymentIntentID: piID,
		Reason:          RefundRequestedByCustomer,
	})
	if err != nil {
		return cancelled, nil, fmt.Errorf("stripe: subscription cancelled but refund failed: %w", err)
	}
	return cancelled, refund, nil
}

// SubscriptionAccessUntil returns the time at which the customer loses access
// to the subscription's features. It handles three states:
//
//   - Scheduled to cancel: returns the period-end date (CancelAt).
//   - Trialing: returns the trial end date.
//   - Active / normal: returns the current period end from the first item.
//
// Returns the zero time.Time if the subscription is already cancelled or nil.
func SubscriptionAccessUntil(sub *Subscription) time.Time {
	if sub == nil {
		return time.Time{}
	}
	if sub.CancelAtPeriodEnd && sub.CancelAt > 0 {
		return time.Unix(sub.CancelAt, 0)
	}
	if sub.Status == sgo.SubscriptionStatusTrialing && sub.TrialEnd > 0 {
		return time.Unix(sub.TrialEnd, 0)
	}
	// CurrentPeriodEnd lives on SubscriptionItem in v85+.
	if sub.Items != nil && len(sub.Items.Data) > 0 {
		if t := sub.Items.Data[0].CurrentPeriodEnd; t > 0 {
			return time.Unix(t, 0)
		}
	}
	return time.Time{}
}

// CouponDuration controls how long a coupon's discount applies.
type CouponDuration string

const (
	// CouponOnce applies the discount only to the next invoice.
	CouponOnce CouponDuration = "once"
	// CouponForever applies the discount indefinitely.
	CouponForever CouponDuration = "forever"
	// CouponRepeating applies for DurationMonths billing cycles.
	CouponRepeating CouponDuration = "repeating"
)

// CouponParams describes a discount coupon. Set either PercentOff or
// AmountOff (with Currency), not both.
type CouponParams struct {
	// ID is optional; leave empty for a Stripe-generated code.
	ID             string
	Name           string
	PercentOff     float64        // e.g. 20 for 20 % off.
	AmountOff      int64          // fixed discount in minor units (use Dollars).
	Currency       string         // required when AmountOff is set.
	Duration       CouponDuration // defaults to CouponOnce.
	DurationMonths int64          // only for CouponRepeating.
	MaxRedemptions int64          // 0 = unlimited.
	Metadata       map[string]string
}

// CreateCoupon creates a reusable discount coupon. Pass the returned
// Coupon.ID to SubscribeParams.CouponID to apply it at checkout.
func (c *Client) CreateCoupon(ctx context.Context, p CouponParams) (*Coupon, error) {
	if p.PercentOff == 0 && p.AmountOff == 0 {
		return nil, fmt.Errorf("stripe: CreateCoupon requires PercentOff or AmountOff")
	}
	if p.Duration == "" {
		p.Duration = CouponOnce
	}
	params := &sgo.CouponCreateParams{
		Duration: String(string(p.Duration)),
	}
	if p.ID != "" {
		params.ID = String(p.ID)
	}
	if p.Name != "" {
		params.Name = String(p.Name)
	}
	if p.PercentOff > 0 {
		params.PercentOff = Float64(p.PercentOff)
	}
	if p.AmountOff > 0 {
		params.AmountOff = Int64(p.AmountOff)
		if p.Currency != "" {
			params.Currency = String(p.Currency)
		}
	}
	if p.Duration == CouponRepeating && p.DurationMonths > 0 {
		params.DurationInMonths = Int64(p.DurationMonths)
	}
	if p.MaxRedemptions > 0 {
		params.MaxRedemptions = Int64(p.MaxRedemptions)
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	c.prep(&params.Params)
	return c.api.V1Coupons.Create(ctx, params)
}

// DeleteCoupon deletes a coupon so it can no longer be applied to new
// subscriptions. Existing discounts are not affected.
func (c *Client) DeleteCoupon(ctx context.Context, couponID string) error {
	params := &sgo.CouponDeleteParams{}
	c.prep(&params.Params)
	_, err := c.api.V1Coupons.Delete(ctx, couponID, params)
	return err
}

// ListSubscriptions returns all subscriptions for a customer.
func (c *Client) ListSubscriptions(ctx context.Context, customerID string) ([]*Subscription, error) {
	params := &sgo.SubscriptionListParams{}
	if customerID != "" {
		params.Customer = String(customerID)
	}
	c.prepList(&params.ListParams)

	var out []*Subscription
	for sub, err := range c.api.V1Subscriptions.List(ctx, params).All(ctx) {
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, nil
}
