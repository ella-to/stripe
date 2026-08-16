package stripe

import (
	"context"
	"fmt"
	"math"
	"strconv"

	sgo "github.com/stripe/stripe-go/v86"
)

// Metadata keys used to persist a connected account's platform fee on the
// account itself, so the fee can be changed at any time and survives restarts
// without you needing a separate database.
const (
	feePercentKey = "platform_fee_percent"
	feeFixedKey   = "platform_fee_fixed"
)

// PlatformFee is the fee your platform charges on a transaction. It can be a
// percentage of the amount, a fixed amount, or both (the two are added
// together). This is collected via Stripe's application fee mechanism on a
// direct charge, which means:
//
//   - the charge settles on the connected account, so the connected account
//     pays Stripe's processing fee, and
//   - your platform receives exactly the application fee, separately.
//
// Example: a $10.00 charge on a connected account, Stripe fee ~ $3.00, and a
// PlatformFee of {Fixed: Dollars(3)} leaves the seller with ~ $4.00 while your
// platform collects $3.00.
type PlatformFee struct {
	Percent float64 // e.g. 2.9 for 2.9%.
	Fixed   int64   // fixed component in minor units (use Dollars).
}

// Compute returns the fee, in minor units, for a transaction of amount minor
// units. The percentage component is rounded to the nearest minor unit.
func (f PlatformFee) Compute(amount int64) int64 {
	fee := f.Fixed
	if f.Percent != 0 {
		fee += int64(math.Round(float64(amount) * f.Percent / 100.0))
	}
	if fee < 0 {
		return 0
	}
	return fee
}

// IsZero reports whether the fee would never collect anything.
func (f PlatformFee) IsZero() bool { return f.Percent == 0 && f.Fixed == 0 }

// FeeResolver looks up the platform fee that applies to a connected account.
// Implement it to source fees from wherever you like - a database, a config
// file, an in-memory map, etc. ok must be false (with a zero fee and nil error)
// when no fee is configured for the account.
//
// The resolver is consulted by ChargeWithFee, Checkout and Subscribe whenever
// an explicit fee is not supplied on the call. Set one with WithFeeResolver.
type FeeResolver interface {
	ResolveFee(ctx context.Context, accountID string) (fee PlatformFee, ok bool, err error)
}

// FeeResolverFunc adapts an ordinary function to the FeeResolver interface.
type FeeResolverFunc func(ctx context.Context, accountID string) (PlatformFee, bool, error)

// ResolveFee implements FeeResolver.
func (f FeeResolverFunc) ResolveFee(ctx context.Context, accountID string) (PlatformFee, bool, error) {
	return f(ctx, accountID)
}

// AccountMetadataFeeStore is the default FeeResolver. It persists each
// connected account's platform fee on the account's own Stripe metadata, so the
// fee can be changed at any time and survives restarts without a separate
// database.
type AccountMetadataFeeStore struct {
	api *sgo.Client
}

// NewAccountMetadataFeeStore returns a metadata-backed fee store using the given
// stripe-go client.
func NewAccountMetadataFeeStore(api *sgo.Client) *AccountMetadataFeeStore {
	return &AccountMetadataFeeStore{api: api}
}

// ResolveFee implements FeeResolver by reading the account's metadata.
func (s *AccountMetadataFeeStore) ResolveFee(ctx context.Context, accountID string) (PlatformFee, bool, error) {
	acct, err := s.api.V1Accounts.GetByID(ctx, accountID, nil)
	if err != nil {
		return PlatformFee{}, false, err
	}
	return platformFeeFromMetadata(acct.Metadata)
}

// SetFee writes the platform fee onto the account's metadata.
func (s *AccountMetadataFeeStore) SetFee(ctx context.Context, accountID string, fee PlatformFee) (*Account, error) {
	params := &sgo.AccountUpdateParams{}
	params.AddMetadata(feePercentKey, strconv.FormatFloat(fee.Percent, 'f', -1, 64))
	params.AddMetadata(feeFixedKey, strconv.FormatInt(fee.Fixed, 10))
	return s.api.V1Accounts.Update(ctx, accountID, params)
}

// SetPlatformFee stores the platform fee for a connected account using the
// default account-metadata store. Call it again at any time to change the fee.
//
// This is a convenience for the metadata-backed default. If you configured a
// custom FeeResolver via WithFeeResolver, manage fee storage through that store
// instead (this method always writes to account metadata).
func (c *Client) SetPlatformFee(ctx context.Context, accountID string, fee PlatformFee) (*Account, error) {
	return NewAccountMetadataFeeStore(c.api).SetFee(ctx, accountID, fee)
}

// GetPlatformFee returns the platform fee resolved for a connected account
// using the client's configured FeeResolver (account metadata by default). ok
// is false when no fee has been configured for the account.
func (c *Client) GetPlatformFee(ctx context.Context, accountID string) (fee PlatformFee, ok bool, err error) {
	if c.feeResolver == nil {
		return PlatformFee{}, false, nil
	}
	return c.feeResolver.ResolveFee(ctx, accountID)
}

func platformFeeFromMetadata(md map[string]string) (PlatformFee, bool, error) {
	pctStr, hasPct := md[feePercentKey]
	fixedStr, hasFixed := md[feeFixedKey]
	if !hasPct && !hasFixed {
		return PlatformFee{}, false, nil
	}
	var fee PlatformFee
	if hasPct && pctStr != "" {
		pct, err := strconv.ParseFloat(pctStr, 64)
		if err != nil {
			return PlatformFee{}, false, fmt.Errorf("stripe: invalid %s metadata %q: %w", feePercentKey, pctStr, err)
		}
		fee.Percent = pct
	}
	if hasFixed && fixedStr != "" {
		fixed, err := strconv.ParseInt(fixedStr, 10, 64)
		if err != nil {
			return PlatformFee{}, false, fmt.Errorf("stripe: invalid %s metadata %q: %w", feeFixedKey, fixedStr, err)
		}
		fee.Fixed = fixed
	}
	return fee, true, nil
}

// resolveFee determines the platform fee to apply to a charge on accountID.
// An explicit fee (non-nil) always wins; otherwise the configured FeeResolver
// is consulted; otherwise a zero fee is returned.
func (c *Client) resolveFee(ctx context.Context, accountID string, explicit *PlatformFee) (PlatformFee, error) {
	if explicit != nil {
		return *explicit, nil
	}
	if accountID == "" || c.feeResolver == nil {
		return PlatformFee{}, nil
	}
	fee, ok, err := c.feeResolver.ResolveFee(ctx, accountID)
	if err != nil {
		return PlatformFee{}, err
	}
	if !ok {
		return PlatformFee{}, nil
	}
	return fee, nil
}

// ChargeParams describes a one-off charge taken on a connected account with a
// platform fee. It creates a direct charge: the connected account is the
// settlement merchant (and pays the Stripe fee), while your platform collects
// the application fee.
type ChargeParams struct {
	ConnectedAccount string // seller account id ("acct_...") - required.
	Amount           int64  // gross amount in minor units (use Dollars).
	Currency         string // defaults to "usd".

	// Fee is the platform fee to apply. When nil, the fee stored for the
	// connected account via SetPlatformFee is used (or none if unset).
	Fee *PlatformFee

	Customer      string // optional customer id on the connected account.
	PaymentMethod string // optional payment method to charge.
	Confirm       bool   // confirm immediately (requires PaymentMethod).
	Description   string
	ReceiptEmail  string
	Metadata      map[string]string
}

// ChargeWithFee creates a direct charge on a connected account and collects the
// platform fee as an application fee. The returned PaymentIntent's
// ApplicationFeeAmount reflects what your platform earns from the charge.
func (c *Client) ChargeWithFee(ctx context.Context, p ChargeParams) (*PaymentIntent, error) {
	if p.ConnectedAccount == "" {
		return nil, fmt.Errorf("stripe: ChargeWithFee requires a ConnectedAccount")
	}
	if p.Amount <= 0 {
		return nil, fmt.Errorf("stripe: ChargeWithFee requires a positive Amount")
	}
	if p.Currency == "" {
		p.Currency = "usd"
	}

	fee, err := c.resolveFee(ctx, p.ConnectedAccount, p.Fee)
	if err != nil {
		return nil, err
	}

	params := &sgo.PaymentIntentCreateParams{
		Amount:   Int64(p.Amount),
		Currency: String(p.Currency),
	}
	if appFee := fee.Compute(p.Amount); appFee > 0 {
		params.ApplicationFeeAmount = Int64(appFee)
	}
	if p.Customer != "" {
		params.Customer = String(p.Customer)
	}
	if p.PaymentMethod != "" {
		params.PaymentMethod = String(p.PaymentMethod)
	}
	if p.Confirm {
		params.Confirm = Bool(true)
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

	// A direct charge is made on behalf of the connected account via the
	// Stripe-Account header, which is what makes the account the settlement
	// merchant that bears the Stripe processing fee.
	eff := c.ForAccount(p.ConnectedAccount)
	eff.prep(&params.Params)
	return eff.api.V1PaymentIntents.Create(ctx, params)
}
