package stripe

import (
	"context"
	"fmt"

	sgo "github.com/stripe/stripe-go/v87"
)

// TrialOfferParams describes an introductory offer: TrialPriceID is charged
// for the first Intervals billing periods, then the subscription item moves to
// ThenPriceID. Use a zero-amount TrialPriceID for a free trial, or a reduced
// one for a paid trial ("$1 for the first 3 months").
//
// Unlike TrialDays, a trial offer is a reusable catalog object: create it once
// (e.g. at startup) and pass its id as SubscribeParams.TrialOfferID.
type TrialOfferParams struct {
	// TrialPriceID is the recurring price charged during the trial. Its
	// interval sets the length of one trial period.
	TrialPriceID string
	// Intervals is how many of TrialPriceID's billing intervals the trial
	// lasts, e.g. 3 with a monthly price for a 3 month trial.
	Intervals int64
	// ThenPriceID is the recurring price the item switches to when the trial
	// ends.
	ThenPriceID string
	// Nickname is an internal label, hidden from customers.
	Nickname string
}

// CreateTrialOffer creates a reusable trial offer (see TrialOfferParams).
func (c *Client) CreateTrialOffer(ctx context.Context, p TrialOfferParams) (*TrialOffer, error) {
	if p.TrialPriceID == "" || p.ThenPriceID == "" || p.Intervals <= 0 {
		return nil, fmt.Errorf("stripe: CreateTrialOffer requires TrialPriceID, ThenPriceID and Intervals > 0")
	}
	params := &sgo.ProductCatalogTrialOfferCreateParams{
		Price: String(p.TrialPriceID),
		Duration: &sgo.ProductCatalogTrialOfferCreateDurationParams{
			Type: String(string(sgo.ProductCatalogTrialOfferDurationTypeRelative)),
			Relative: &sgo.ProductCatalogTrialOfferCreateDurationRelativeParams{
				Iterations: Int64(p.Intervals),
			},
		},
		EndBehavior: &sgo.ProductCatalogTrialOfferCreateEndBehaviorParams{
			Transition: &sgo.ProductCatalogTrialOfferCreateEndBehaviorTransitionParams{
				Price: String(p.ThenPriceID),
			},
		},
	}
	if p.Nickname != "" {
		params.Nickname = String(p.Nickname)
	}
	c.prep(&params.Params)
	return c.api.V1ProductCatalogTrialOffers.Create(ctx, params)
}

// GetTrialOffer retrieves a trial offer by id.
func (c *Client) GetTrialOffer(ctx context.Context, id string) (*TrialOffer, error) {
	params := &sgo.ProductCatalogTrialOfferRetrieveParams{}
	c.prep(&params.Params)
	return c.api.V1ProductCatalogTrialOffers.Retrieve(ctx, id, params)
}

// ListTrialOffers returns the active trial offers. When trialPriceID is not
// empty, only offers whose trial price is trialPriceID are returned.
func (c *Client) ListTrialOffers(ctx context.Context, trialPriceID string) ([]*TrialOffer, error) {
	params := &sgo.ProductCatalogTrialOfferListParams{Active: Bool(true)}
	if trialPriceID != "" {
		params.Prices = stringSlice([]string{trialPriceID})
	}
	c.prepList(&params.ListParams)
	var out []*TrialOffer
	for offer, err := range c.api.V1ProductCatalogTrialOffers.List(ctx, params).All(ctx) {
		if err != nil {
			return nil, err
		}
		out = append(out, offer)
	}
	return out, nil
}

// ArchiveTrialOffer deactivates a trial offer so it can no longer be applied
// to new subscriptions. Subscriptions already on the offer are unaffected.
// Trial offers cannot be deleted.
func (c *Client) ArchiveTrialOffer(ctx context.Context, id string) (*TrialOffer, error) {
	params := &sgo.ProductCatalogTrialOfferUpdateParams{Active: Bool(false)}
	c.prep(&params.Params)
	return c.api.V1ProductCatalogTrialOffers.Update(ctx, id, params)
}
