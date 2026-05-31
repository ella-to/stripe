package stripe

import (
	"context"
	"fmt"
	"time"

	sgo "github.com/stripe/stripe-go/v85"
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
