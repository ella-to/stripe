// Package stripe is a thin, opinionated wrapper around the official stripe-go
// SDK (github.com/stripe/stripe-go/v86). It collapses the most common Stripe
// integration patterns - Connect onboarding, subscriptions, usage based
// quotas, one-off purchases, webhooks and tax - into a small, task oriented
// API so that the caller does not have to assemble low level parameter structs
// by hand.
//
// The wrapper never hides the underlying types: every method returns the real
// stripe-go resource (re-exported here for convenience, see types.go) so you
// can always drop down to the full SDK when you need something the wrapper does
// not cover.
package stripe

import (
	"math"

	sgo "github.com/stripe/stripe-go/v86"
)

// Client is the entry point for every feature in this package. It wraps a
// *stripe.Client from the official SDK and remembers a few pieces of
// configuration (webhook secret, OAuth client id) so callers do not have to
// thread them through every call.
//
// A Client is safe for concurrent use. Use ForAccount to obtain a copy scoped
// to a connected account.
type Client struct {
	api              *sgo.Client
	apiKey           string
	webhookSecret    string
	oauthClientID    string
	connectedAccount string
	feeResolver      FeeResolver

	// Populated when the client authenticates via a Connect OAuth access token
	// (see NewFromOAuthToken): the account the token represents and the refresh
	// token, if any.
	oauthAccountID   string
	oauthRefresh     string
	oauthRedirectURI string
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithWebhookSecret sets the signing secret used to verify incoming webhook
// payloads (the value that starts with "whsec_"). It is used as the default
// secret when creating a webhook Dispatcher via Client.Webhooks.
func WithWebhookSecret(secret string) Option {
	return func(c *Client) { c.webhookSecret = secret }
}

// WithOAuthClientID sets the Connect OAuth application client id (starts with
// "ca_"). It is required for ConnectAuthorizeURL.
func WithOAuthClientID(id string) Option {
	return func(c *Client) { c.oauthClientID = id }
}

// WithOAuthRedirectURI sets the redirect URI sent in the OAuth authorization
// request. It must match one of the URIs registered in your Stripe Connect
// settings. Required for ConnectAuthorizeURL.
func WithOAuthRedirectURI(uri string) Option {
	return func(c *Client) { c.oauthRedirectURI = uri }
}

// WithStripeClient lets callers inject a fully customised *stripe.Client (for
// example one configured with custom backends for testing). When supplied it
// takes precedence over the api key for building requests.
func WithStripeClient(sc *sgo.Client) Option {
	return func(c *Client) {
		if sc != nil {
			c.api = sc
		}
	}
}

// WithFeeResolver sets the strategy used to look up the platform fee for a
// connected account when an explicit fee is not provided to a charge. The
// default (when this option is not used) is AccountMetadataFeeStore, which
// reads the fee from the connected account's Stripe metadata. Supply your own
// FeeResolver (e.g. backed by a database) for full flexibility.
func WithFeeResolver(r FeeResolver) Option {
	return func(c *Client) { c.feeResolver = r }
}

// New creates a Client from a secret API key (starts with "sk_").
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		api:    sgo.NewClient(apiKey),
		apiKey: apiKey,
	}
	for _, o := range opts {
		o(c)
	}
	if c.feeResolver == nil {
		c.feeResolver = NewAccountMetadataFeeStore(c.api)
	}
	return c
}

// NewFromOAuthToken builds a Client that authenticates as the account the OAuth
// token was issued for, using the token's AccessToken as the API key instead of
// a platform secret key. Every call made through the returned client then
// operates as that account.
//
// This is the basis for a multi-tenant SaaS where each tenant connects their
// own Stripe account via Connect OAuth (see Client.ConnectAuthorizeURL and
// Client.ClientFromOAuthCode) rather than handing you a secret key.
func NewFromOAuthToken(tok *OAuthToken, opts ...Option) *Client {
	c := New(tok.AccessToken, opts...)
	c.oauthAccountID = tok.StripeUserID
	c.oauthRefresh = tok.RefreshToken
	return c
}

// Raw exposes the underlying official stripe-go client for advanced use cases
// not covered by this wrapper.
func (c *Client) Raw() *sgo.Client { return c.api }

// AccountID returns the Stripe account id this client authenticates as when it
// was built from an OAuth token (NewFromOAuthToken); it is empty for a client
// built from a platform secret key.
func (c *Client) AccountID() string { return c.oauthAccountID }

// RefreshToken returns the OAuth refresh token captured at construction, if any.
func (c *Client) RefreshToken() string { return c.oauthRefresh }

// ForAccount returns a shallow copy of the client scoped to a connected
// account. Every request issued through the returned client is made on behalf
// of accountID via the Stripe-Account header, which is the recommended way to
// act as a connected account.
//
//	platform := stripe.New(key)
//	seller := platform.ForAccount("acct_123")
//	seller.CreateTaxRate(ctx, ...) // created on the connected account
func (c *Client) ForAccount(accountID string) *Client {
	cp := *c
	cp.connectedAccount = accountID
	return &cp
}

// prep applies client-wide settings (currently the connected account header)
// to an outgoing parameter struct. Every wrapper method funnels its params
// through prep so ForAccount works uniformly.
func (c *Client) prep(p *sgo.Params) {
	if c.connectedAccount != "" {
		p.SetStripeAccount(c.connectedAccount)
	}
}

// prepList is the *ListParams equivalent of prep, used for list endpoints.
func (c *Client) prepList(p *sgo.ListParams) {
	if c.connectedAccount != "" {
		p.SetStripeAccount(c.connectedAccount)
	}
}

// --- small value helpers -------------------------------------------------
//
// stripe-go takes pointers for optional fields. These helpers keep call sites
// readable without importing the upstream package directly.

// String returns a pointer to v.
func String(v string) *string { return &v }

// Int64 returns a pointer to v.
func Int64(v int64) *int64 { return &v }

// Bool returns a pointer to v.
func Bool(v bool) *bool { return &v }

// Float64 returns a pointer to v.
func Float64(v float64) *float64 { return &v }

// Dollars converts a human readable amount (e.g. 19.99) into the integer
// minor-unit amount Stripe expects (1999). It rounds to the nearest cent.
func Dollars(amount float64) int64 {
	return int64(math.Round(amount * 100))
}

// stringSlice converts a slice of strings into the []*string form the SDK uses.
func stringSlice(in []string) []*string {
	if len(in) == 0 {
		return nil
	}
	out := make([]*string, len(in))
	for i := range in {
		out[i] = String(in[i])
	}
	return out
}
