package stripe

import (
	"context"
	"fmt"

	sgo "github.com/stripe/stripe-go/v85"
)

// RefundReason is the reason code for a refund, as required by Stripe.
type RefundReason string

const (
	RefundDuplicate          RefundReason = "duplicate"
	RefundFraudulent         RefundReason = "fraudulent"
	RefundRequestedByCustomer RefundReason = "requested_by_customer"
)

// RefundParams describes a full or partial refund of a captured payment.
type RefundParams struct {
	// PaymentIntentID is the payment intent to refund (pi_...). Required.
	PaymentIntentID string
	// Amount is the amount to refund in minor units. Zero means a full refund.
	Amount int64
	Reason RefundReason
	Metadata map[string]string
}

// RefundPayment issues a full or partial refund against a PaymentIntent. The
// returned Refund includes its status; "succeeded" means the money is on its
// way back to the customer.
func (c *Client) RefundPayment(ctx context.Context, p RefundParams) (*Refund, error) {
	if p.PaymentIntentID == "" {
		return nil, fmt.Errorf("stripe: RefundPayment requires a PaymentIntentID")
	}
	params := &sgo.RefundCreateParams{
		PaymentIntent: String(p.PaymentIntentID),
	}
	if p.Amount > 0 {
		params.Amount = Int64(p.Amount)
	}
	if p.Reason != "" {
		params.Reason = String(string(p.Reason))
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	c.prep(&params.Params)
	return c.api.V1Refunds.Create(ctx, params)
}

// CartItem is a single line on a purchase. Provide either PriceID (to reference
// an existing Stripe price) or Name+Amount (to define the price inline).
type CartItem struct {
	// Inline price definition.
	Name        string
	Description string
	Amount      int64 // unit amount in minor units (use Dollars).
	Images      []string

	// Or reference an existing price instead of the inline fields above.
	PriceID string

	Quantity int64 // defaults to 1.

	// TaxRateIDs applies specific tax rates to this line (manual tax). Ignored
	// when automatic tax is enabled on the cart.
	TaxRateIDs []string
}

type shippingRate struct {
	name   string
	amount int64
}

// Cart accumulates items, tax and shipping for a one-off purchase. Build it up
// (items first, tax and shipping can be added later) and pass it to Checkout.
type Cart struct {
	currency     string
	items        []CartItem
	shipping     []shippingRate
	automaticTax bool
}

// NewCart starts an empty cart. currency applies to inline items and shipping
// (defaults to "usd" when empty).
func NewCart(currency string) *Cart {
	if currency == "" {
		currency = "usd"
	}
	return &Cart{currency: currency}
}

// Add appends a fully specified item.
func (c *Cart) Add(item CartItem) *Cart {
	c.items = append(c.items, item)
	return c
}

// AddItem appends an inline-priced item (name + unit amount + quantity).
func (c *Cart) AddItem(name string, amount, quantity int64) *Cart {
	return c.Add(CartItem{Name: name, Amount: amount, Quantity: quantity})
}

// AddPrice appends an item that references an existing Stripe price.
func (c *Cart) AddPrice(priceID string, quantity int64) *Cart {
	return c.Add(CartItem{PriceID: priceID, Quantity: quantity})
}

// WithAutomaticTax turns on Stripe Tax, which calculates and adds tax at
// checkout based on the customer's location. Requires Stripe Tax to be enabled
// on the account.
func (c *Cart) WithAutomaticTax() *Cart {
	c.automaticTax = true
	return c
}

// AddShipping adds a flat-rate shipping option the customer can pick at
// checkout. Call multiple times to offer several options.
func (c *Cart) AddShipping(name string, amount int64) *Cart {
	c.shipping = append(c.shipping, shippingRate{name: name, amount: amount})
	return c
}

// Total returns the sum of the inline-priced items in minor units. Items that
// reference an existing PriceID are not included (their amount is not known
// locally); allInline is false when any such item is present. This is used to
// size a percentage based platform fee at checkout.
func (c *Cart) Total() (total int64, allInline bool) {
	allInline = true
	for _, it := range c.items {
		if it.PriceID != "" {
			allInline = false
			continue
		}
		qty := it.Quantity
		if qty == 0 {
			qty = 1
		}
		total += it.Amount * qty
	}
	return total, allInline
}

// CheckoutParams configures a hosted Checkout session for a cart.
type CheckoutParams struct {
	Cart          *Cart
	SuccessURL    string
	CancelURL     string
	Customer      string // optional existing customer id.
	CustomerEmail string // optional; prefilled on the page.
	Metadata      map[string]string

	// ConnectedAccount, when set, turns the checkout into a direct charge on
	// that connected account ("acct_..."): the account becomes the settlement
	// merchant (and pays the Stripe processing fee), while your platform
	// collects the application fee below.
	ConnectedAccount string

	// Fee is the platform fee to collect. When ConnectedAccount is set and Fee
	// is nil, the fee stored for the account via SetPlatformFee is used.
	// A percentage fee is sized against the cart's inline item Total.
	Fee *PlatformFee

	// FeeAmount overrides the computed fee with an explicit amount in minor
	// units. Use it when the cart references prices whose totals are not known
	// locally. It takes precedence over Fee.
	FeeAmount int64
}

// Checkout creates a hosted Stripe Checkout session for the cart and returns
// it. Redirect the buyer to CheckoutSession.URL to collect payment.
func (c *Client) Checkout(ctx context.Context, p CheckoutParams) (*CheckoutSession, error) {
	if p.Cart == nil || len(p.Cart.items) == 0 {
		return nil, fmt.Errorf("stripe: Checkout requires a non-empty Cart")
	}
	if p.SuccessURL == "" || p.CancelURL == "" {
		return nil, fmt.Errorf("stripe: Checkout requires SuccessURL and CancelURL")
	}

	cart := p.Cart
	params := &sgo.CheckoutSessionCreateParams{
		Mode:       String("payment"),
		SuccessURL: String(p.SuccessURL),
		CancelURL:  String(p.CancelURL),
	}
	if p.Customer != "" {
		params.Customer = String(p.Customer)
	}
	if p.CustomerEmail != "" {
		params.CustomerEmail = String(p.CustomerEmail)
	}

	// A direct charge with a platform (application) fee is made on behalf of the
	// connected account; eff carries the Stripe-Account header.
	eff := c
	if p.ConnectedAccount != "" {
		eff = c.ForAccount(p.ConnectedAccount)

		appFee := p.FeeAmount
		if appFee == 0 {
			fee, err := c.resolveFee(ctx, p.ConnectedAccount, p.Fee)
			if err != nil {
				return nil, err
			}
			total, _ := cart.Total()
			appFee = fee.Compute(total)
		}
		if appFee > 0 {
			params.PaymentIntentData = &sgo.CheckoutSessionCreatePaymentIntentDataParams{
				ApplicationFeeAmount: Int64(appFee),
			}
		}
	}

	for _, it := range cart.items {
		qty := it.Quantity
		if qty == 0 {
			qty = 1
		}
		li := &sgo.CheckoutSessionCreateLineItemParams{Quantity: Int64(qty)}
		switch {
		case it.PriceID != "":
			li.Price = String(it.PriceID)
		default:
			li.PriceData = &sgo.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   String(cart.currency),
				UnitAmount: Int64(it.Amount),
				ProductData: &sgo.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name: String(it.Name),
				},
			}
			if it.Description != "" {
				li.PriceData.ProductData.Description = String(it.Description)
			}
			if len(it.Images) > 0 {
				li.PriceData.ProductData.Images = stringSlice(it.Images)
			}
		}
		if len(it.TaxRateIDs) > 0 && !cart.automaticTax {
			li.TaxRates = stringSlice(it.TaxRateIDs)
		}
		params.LineItems = append(params.LineItems, li)
	}

	if cart.automaticTax {
		params.AutomaticTax = &sgo.CheckoutSessionCreateAutomaticTaxParams{Enabled: Bool(true)}
	}

	for _, sr := range cart.shipping {
		params.ShippingOptions = append(params.ShippingOptions, &sgo.CheckoutSessionCreateShippingOptionParams{
			ShippingRateData: &sgo.CheckoutSessionCreateShippingOptionShippingRateDataParams{
				DisplayName: String(sr.name),
				Type:        String("fixed_amount"),
				FixedAmount: &sgo.CheckoutSessionCreateShippingOptionShippingRateDataFixedAmountParams{
					Amount:   Int64(sr.amount),
					Currency: String(cart.currency),
				},
			},
		})
	}

	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}

	eff.prep(&params.Params)
	return eff.api.V1CheckoutSessions.Create(ctx, params)
}
