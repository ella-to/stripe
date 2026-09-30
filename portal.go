package stripe

import (
	"context"
	"fmt"

	sgo "github.com/stripe/stripe-go/v86"
)

// PortalParams opens the Stripe hosted customer portal, where customers can
// update their card, download invoices, and switch or cancel plans without you
// building any UI.
type PortalParams struct {
	Customer  string // customer id ("cus_...") - required.
	ReturnURL string // where the "back" link in the portal goes - required.
	// ConfigurationID selects a configuration from CreatePortalConfiguration.
	// When empty the account's default configuration (Dashboard > Settings >
	// Billing > Customer portal) is used.
	ConfigurationID string
}

// CustomerPortal creates a single-use customer portal session. Redirect the
// customer to BillingPortalSession.URL. Changes they make there arrive as
// customer.subscription.* webhooks.
func (c *Client) CustomerPortal(ctx context.Context, p PortalParams) (*BillingPortalSession, error) {
	if p.Customer == "" || p.ReturnURL == "" {
		return nil, fmt.Errorf("stripe: CustomerPortal requires Customer and ReturnURL")
	}
	params := &sgo.BillingPortalSessionCreateParams{
		Customer:  String(p.Customer),
		ReturnURL: String(p.ReturnURL),
	}
	if p.ConfigurationID != "" {
		params.Configuration = String(p.ConfigurationID)
	}
	c.prep(&params.Params)
	return c.api.V1BillingPortalSessions.Create(ctx, params)
}

// PortalConfigParams describes what customers may do in the portal. It is an
// alternative to configuring the portal in the Dashboard; a test mode account
// has no default configuration until you save one there.
type PortalConfigParams struct {
	Headline  string // optional text shown at the top of the portal.
	ReturnURL string // optional default for PortalParams.ReturnURL.

	AllowUpdatePaymentMethod bool
	AllowInvoiceHistory      bool
	// AllowUpdateDetails lets customers edit their email and billing address.
	AllowUpdateDetails bool

	// AllowCancel lets customers cancel. They keep access until the end of the
	// paid period unless CancelImmediately is set.
	AllowCancel       bool
	CancelImmediately bool

	// SwitchPrices are the recurring price ids customers can switch between
	// (e.g. monthly and yearly, or Basic and Pro). Leave empty to disable plan
	// switching. Changes are prorated.
	SwitchPrices []string
}

// CreatePortalConfiguration creates a customer portal configuration and
// returns it; pass its ID as PortalParams.ConfigurationID.
func (c *Client) CreatePortalConfiguration(ctx context.Context, p PortalConfigParams) (*BillingPortalConfiguration, error) {
	features := &sgo.BillingPortalConfigurationCreateFeaturesParams{
		PaymentMethodUpdate: &sgo.BillingPortalConfigurationCreateFeaturesPaymentMethodUpdateParams{
			Enabled: Bool(p.AllowUpdatePaymentMethod),
		},
		InvoiceHistory: &sgo.BillingPortalConfigurationCreateFeaturesInvoiceHistoryParams{
			Enabled: Bool(p.AllowInvoiceHistory),
		},
		CustomerUpdate: &sgo.BillingPortalConfigurationCreateFeaturesCustomerUpdateParams{
			Enabled: Bool(p.AllowUpdateDetails),
		},
		SubscriptionCancel: &sgo.BillingPortalConfigurationCreateFeaturesSubscriptionCancelParams{
			Enabled: Bool(p.AllowCancel),
		},
	}
	if p.AllowUpdateDetails {
		features.CustomerUpdate.AllowedUpdates = stringSlice([]string{"email", "address"})
	}
	if p.AllowCancel {
		mode := "at_period_end"
		if p.CancelImmediately {
			mode = "immediately"
		}
		features.SubscriptionCancel.Mode = String(mode)
	}

	if len(p.SwitchPrices) > 0 {
		// The portal groups prices by product, so look up each price's product.
		byProduct := map[string][]string{}
		var order []string
		for _, id := range p.SwitchPrices {
			rp := &sgo.PriceRetrieveParams{}
			c.prep(&rp.Params)
			price, err := c.api.V1Prices.Retrieve(ctx, id, rp)
			if err != nil {
				return nil, fmt.Errorf("stripe: look up price %s: %w", id, err)
			}
			if price.Product == nil {
				return nil, fmt.Errorf("stripe: price %s has no product", id)
			}
			pid := price.Product.ID
			if _, seen := byProduct[pid]; !seen {
				order = append(order, pid)
			}
			byProduct[pid] = append(byProduct[pid], id)
		}
		update := &sgo.BillingPortalConfigurationCreateFeaturesSubscriptionUpdateParams{
			Enabled:               Bool(true),
			DefaultAllowedUpdates: stringSlice([]string{"price"}),
			ProrationBehavior:     String("create_prorations"),
		}
		for _, pid := range order {
			update.Products = append(update.Products, &sgo.BillingPortalConfigurationCreateFeaturesSubscriptionUpdateProductParams{
				Product: String(pid),
				Prices:  stringSlice(byProduct[pid]),
			})
		}
		features.SubscriptionUpdate = update
	}

	params := &sgo.BillingPortalConfigurationCreateParams{Features: features}
	if p.Headline != "" {
		params.BusinessProfile = &sgo.BillingPortalConfigurationCreateBusinessProfileParams{Headline: String(p.Headline)}
	}
	if p.ReturnURL != "" {
		params.DefaultReturnURL = String(p.ReturnURL)
	}
	c.prep(&params.Params)
	return c.api.V1BillingPortalConfigurations.Create(ctx, params)
}
