package stripe

import (
	"context"
	"fmt"
	"strconv"
	"time"

	sgo "github.com/stripe/stripe-go/v86"
)

// Aggregation describes how raw usage events are rolled up by a meter.
type Aggregation string

const (
	// AggSum adds up the "value" of every reported event.
	AggSum Aggregation = "sum"
	// AggCount counts events regardless of value.
	AggCount Aggregation = "count"
)

// MeterParams describes a usage meter. A meter is the bucket that raw usage
// events flow into; a metered Price then turns aggregated usage into money.
type MeterParams struct {
	DisplayName string
	// EventName is the identifier you report usage against (see ReportUsage).
	EventName string
	// Aggregation defaults to AggSum.
	Aggregation Aggregation
}

// CreateMeter creates a usage meter. Stripe will aggregate every usage event
// reported with the same EventName into this meter.
func (c *Client) CreateMeter(ctx context.Context, p MeterParams) (*BillingMeter, error) {
	if p.EventName == "" {
		return nil, fmt.Errorf("stripe: CreateMeter requires an EventName")
	}
	if p.Aggregation == "" {
		p.Aggregation = AggSum
	}
	params := &sgo.BillingMeterCreateParams{
		DisplayName: String(p.DisplayName),
		EventName:   String(p.EventName),
		DefaultAggregation: &sgo.BillingMeterCreateDefaultAggregationParams{
			Formula: String(string(p.Aggregation)),
		},
	}
	c.prep(&params.Params)
	return c.api.V1BillingMeters.Create(ctx, params)
}

// MeteredPriceParams models "package" pricing with automatic overage, e.g.
// "1000 API requests cost $2, and every additional 1000 costs another $2".
//
// It produces a recurring metered price where the charge is
//
//	ceil(usage / PackageSize) * AmountPerPackage
//
// so going from 1000 to 1001 units rolls into the next package and adds another
// AmountPerPackage to the bill automatically.
type MeteredPriceParams struct {
	ProductName      string
	MeterID          string   // id from CreateMeter.
	AmountPerPackage int64    // price of one package in minor units (use Dollars).
	PackageSize      int64    // units per package, e.g. 1000.
	Currency         string   // defaults to "usd".
	Interval         Interval // billing cadence, defaults to Monthly.
}

// CreateMeteredPrice creates the package/overage price described by p.
func (c *Client) CreateMeteredPrice(ctx context.Context, p MeteredPriceParams) (*Price, error) {
	if p.MeterID == "" {
		return nil, fmt.Errorf("stripe: CreateMeteredPrice requires a MeterID")
	}
	if p.PackageSize <= 0 {
		return nil, fmt.Errorf("stripe: CreateMeteredPrice requires PackageSize > 0")
	}
	if p.Currency == "" {
		p.Currency = "usd"
	}
	if p.Interval == "" {
		p.Interval = Monthly
	}

	params := &sgo.PriceCreateParams{
		Currency:   String(p.Currency),
		UnitAmount: Int64(p.AmountPerPackage),
		Recurring: &sgo.PriceCreateRecurringParams{
			Interval:  String(string(p.Interval)),
			UsageType: String("metered"),
			Meter:     String(p.MeterID),
		},
		TransformQuantity: &sgo.PriceCreateTransformQuantityParams{
			DivideBy: Int64(p.PackageSize),
			Round:    String("up"),
		},
		ProductData: &sgo.PriceCreateProductDataParams{
			Name: String(p.ProductName),
		},
	}
	c.prep(&params.Params)
	return c.api.V1Prices.Create(ctx, params)
}

// ReportUsage records usage for a customer against a meter's EventName. Call it
// every time the customer consumes the metered resource (e.g. makes an API
// request). Stripe aggregates these events and bills them on the next invoice.
func (c *Client) ReportUsage(ctx context.Context, customerID, eventName string, quantity int64) (*BillingMeterEvent, error) {
	params := &sgo.BillingMeterEventCreateParams{
		EventName: String(eventName),
		Payload: map[string]string{
			"stripe_customer_id": customerID,
			"value":              strconv.FormatInt(quantity, 10),
		},
	}
	c.prep(&params.Params)
	return c.api.V1BillingMeterEvents.Create(ctx, params)
}

// QuotaGrantParams describes a prepaid, optionally expiring, pool of credit for
// a customer. This implements the "monthly quota that expires and renews"
// pattern: grant a credit that covers the included usage and expires at the end
// of the cycle; usage beyond it is billed as overage on the metered price.
//
// To renew, grant again at the start of each period (a good place to do this is
// from an invoice.paid webhook - see the Webhook dispatcher).
type QuotaGrantParams struct {
	Customer  string
	Amount    int64         // monetary value of the credit in minor units.
	Currency  string        // defaults to "usd".
	ExpiresIn time.Duration // optional; e.g. 30*24*time.Hour for a monthly grant.
	Name      string        // optional human label.
	// PriceIDs optionally restricts which prices the credit applies to. When
	// empty the credit applies to all metered prices for the customer.
	PriceIDs []string
	Metadata map[string]string
}

// GrantQuota issues a (possibly expiring) credit balance to a customer.
func (c *Client) GrantQuota(ctx context.Context, p QuotaGrantParams) (*BillingCreditGrant, error) {
	if p.Customer == "" {
		return nil, fmt.Errorf("stripe: GrantQuota requires a Customer")
	}
	if p.Currency == "" {
		p.Currency = "usd"
	}

	scope := &sgo.BillingCreditGrantCreateApplicabilityConfigScopeParams{}
	if len(p.PriceIDs) > 0 {
		for _, id := range p.PriceIDs {
			scope.Prices = append(scope.Prices, &sgo.BillingCreditGrantCreateApplicabilityConfigScopePriceParams{
				ID: String(id),
			})
		}
	} else {
		scope.PriceType = String("metered")
	}

	params := &sgo.BillingCreditGrantCreateParams{
		Customer: String(p.Customer),
		Category: String(string(sgo.BillingCreditGrantCategoryPromotional)),
		Amount: &sgo.BillingCreditGrantCreateAmountParams{
			Type: String("monetary"),
			Monetary: &sgo.BillingCreditGrantCreateAmountMonetaryParams{
				Currency: String(p.Currency),
				Value:    Int64(p.Amount),
			},
		},
		ApplicabilityConfig: &sgo.BillingCreditGrantCreateApplicabilityConfigParams{
			Scope: scope,
		},
	}
	if p.Name != "" {
		params.Name = String(p.Name)
	}
	if p.ExpiresIn > 0 {
		params.ExpiresAt = Int64(time.Now().Add(p.ExpiresIn).Unix())
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	c.prep(&params.Params)
	return c.api.V1BillingCreditGrants.Create(ctx, params)
}

// MeteredQuotaPlan bundles the resources created by SetupMeteredQuota.
type MeteredQuotaPlan struct {
	Meter *BillingMeter
	Price *Price
}

// SetupMeteredQuotaParams is the one-call setup for a usage based quota product.
type SetupMeteredQuotaParams struct {
	ProductName      string
	EventName        string      // usage event identifier.
	Aggregation      Aggregation // defaults to AggSum.
	AmountPerPackage int64       // price per package, minor units.
	PackageSize      int64       // units per package, e.g. 1000.
	Currency         string      // defaults to "usd".
	Interval         Interval    // defaults to Monthly.
}

// VoidCreditGrant cancels an active credit grant so the customer can no longer
// redeem it. Use this when a customer cancels their quota plan mid-cycle and
// you want to immediately remove any remaining prepaid credit.
func (c *Client) VoidCreditGrant(ctx context.Context, grantID string) (*BillingCreditGrant, error) {
	if grantID == "" {
		return nil, fmt.Errorf("stripe: VoidCreditGrant requires a grantID")
	}
	params := &sgo.BillingCreditGrantVoidGrantParams{}
	c.prep(&params.Params)
	return c.api.V1BillingCreditGrants.VoidGrant(ctx, grantID, params)
}

// SetupMeteredQuota creates the meter and the metered/package price together.
// Subscribe a customer to the returned Price, then call ReportUsage as they
// consume the resource.
func (c *Client) SetupMeteredQuota(ctx context.Context, p SetupMeteredQuotaParams) (*MeteredQuotaPlan, error) {
	meter, err := c.CreateMeter(ctx, MeterParams{
		DisplayName: p.ProductName,
		EventName:   p.EventName,
		Aggregation: p.Aggregation,
	})
	if err != nil {
		return nil, fmt.Errorf("stripe: create meter: %w", err)
	}
	price, err := c.CreateMeteredPrice(ctx, MeteredPriceParams{
		ProductName:      p.ProductName,
		MeterID:          meter.ID,
		AmountPerPackage: p.AmountPerPackage,
		PackageSize:      p.PackageSize,
		Currency:         p.Currency,
		Interval:         p.Interval,
	})
	if err != nil {
		return nil, fmt.Errorf("stripe: create metered price: %w", err)
	}
	return &MeteredQuotaPlan{Meter: meter, Price: price}, nil
}
