package stripe

import (
	"context"
	"fmt"

	sgo "github.com/stripe/stripe-go/v85"
)

// TaxRateParams describes a tax rate for a particular jurisdiction. Tax rates
// can be attached to invoices, subscriptions and Checkout line items.
type TaxRateParams struct {
	DisplayName  string  // shown to customers, e.g. "VAT" or "CA Sales Tax".
	Percentage   float64 // e.g. 7.25 for 7.25%.
	Inclusive    bool    // true if the rate is included in the price.
	Country      string  // ISO country code, e.g. "US", "DE".
	State        string  // state/province code where applicable, e.g. "CA".
	Jurisdiction string  // human readable jurisdiction label.
	Description  string
	// TaxType is the kind of tax, e.g. "vat", "sales_tax", "gst".
	TaxType  string
	Metadata map[string]string
}

// CreateTaxRate creates a tax rate on the platform account.
//
// To create a tax rate that belongs to a connected account, scope the client
// first:
//
//	platform.ForAccount("acct_123").CreateTaxRate(ctx, params)
func (c *Client) CreateTaxRate(ctx context.Context, p TaxRateParams) (*TaxRate, error) {
	if p.DisplayName == "" {
		return nil, fmt.Errorf("stripe: CreateTaxRate requires a DisplayName")
	}
	params := &sgo.TaxRateCreateParams{
		DisplayName: String(p.DisplayName),
		Percentage:  Float64(p.Percentage),
		Inclusive:   Bool(p.Inclusive),
	}
	if p.Country != "" {
		params.Country = String(p.Country)
	}
	if p.State != "" {
		params.State = String(p.State)
	}
	if p.Jurisdiction != "" {
		params.Jurisdiction = String(p.Jurisdiction)
	}
	if p.Description != "" {
		params.Description = String(p.Description)
	}
	if p.TaxType != "" {
		params.TaxType = String(p.TaxType)
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	c.prep(&params.Params)
	return c.api.V1TaxRates.Create(ctx, params)
}

// CreateTaxRateForAccount is a convenience wrapper that creates the tax rate on
// the given connected account.
func (c *Client) CreateTaxRateForAccount(ctx context.Context, accountID string, p TaxRateParams) (*TaxRate, error) {
	return c.ForAccount(accountID).CreateTaxRate(ctx, p)
}

// ListTaxRates returns all tax rates, optionally only the active ones.
func (c *Client) ListTaxRates(ctx context.Context, activeOnly bool) ([]*TaxRate, error) {
	params := &sgo.TaxRateListParams{}
	if activeOnly {
		params.Active = Bool(true)
	}
	c.prepList(&params.ListParams)

	var out []*TaxRate
	for tr, err := range c.api.V1TaxRates.List(ctx, params).All(ctx) {
		if err != nil {
			return nil, err
		}
		out = append(out, tr)
	}
	return out, nil
}

// DeactivateTaxRate archives a tax rate so it can no longer be applied. Stripe
// tax rates are immutable other than their active flag and metadata, so this is
// how you "delete" one.
func (c *Client) DeactivateTaxRate(ctx context.Context, id string) (*TaxRate, error) {
	params := &sgo.TaxRateUpdateParams{Active: Bool(false)}
	c.prep(&params.Params)
	return c.api.V1TaxRates.Update(ctx, id, params)
}
