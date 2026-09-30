package stripe

import (
	"context"
	"fmt"

	sgo "github.com/stripe/stripe-go/v86"
)

// ChargeCustomerParams describes a one-off charge against a customer's saved
// payment method, taken on your own account (no Connect). Use it for add-ons,
// top-ups or usage you bill yourself.
type ChargeCustomerParams struct {
	Customer string // customer id ("cus_...") - required.
	Amount   int64  // amount in minor units (use Dollars) - required.
	Currency string // defaults to "usd".

	// PaymentMethod to charge. Defaults to the customer's default payment
	// method (see CreateCustomerParams.PaymentMethod / AttachPaymentMethod).
	PaymentMethod string

	Description  string
	ReceiptEmail string
	Metadata     map[string]string

	// IdempotencyKey makes retries safe: Stripe returns the original result
	// instead of charging twice. Use something tied to the thing being paid
	// for, e.g. "order-1001".
	IdempotencyKey string
}

// ChargeCustomer charges a customer's saved payment method immediately
// (off-session, no customer interaction). The returned PaymentIntent has
// Status "succeeded" on success; a declined card is returned as an error.
func (c *Client) ChargeCustomer(ctx context.Context, p ChargeCustomerParams) (*PaymentIntent, error) {
	if p.Customer == "" {
		return nil, fmt.Errorf("stripe: ChargeCustomer requires a Customer")
	}
	if p.Amount <= 0 {
		return nil, fmt.Errorf("stripe: ChargeCustomer requires a positive Amount")
	}
	if p.Currency == "" {
		p.Currency = "usd"
	}

	pm := p.PaymentMethod
	if pm == "" {
		cus, err := c.GetCustomer(ctx, p.Customer)
		if err != nil {
			return nil, err
		}
		if cus.InvoiceSettings == nil || cus.InvoiceSettings.DefaultPaymentMethod == nil {
			return nil, fmt.Errorf("stripe: customer %s has no default payment method (see AttachPaymentMethod)", p.Customer)
		}
		pm = cus.InvoiceSettings.DefaultPaymentMethod.ID
	}

	params := &sgo.PaymentIntentCreateParams{
		Amount:        Int64(p.Amount),
		Currency:      String(p.Currency),
		Customer:      String(p.Customer),
		PaymentMethod: String(pm),
		Confirm:       Bool(true),
		OffSession:    Bool(true),
	}
	if p.Description != "" {
		params.Description = String(p.Description)
	}
	if p.ReceiptEmail != "" {
		params.ReceiptEmail = String(p.ReceiptEmail)
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	if p.IdempotencyKey != "" {
		params.SetIdempotencyKey(p.IdempotencyKey)
	}
	c.prep(&params.Params)
	return c.api.V1PaymentIntents.Create(ctx, params)
}
