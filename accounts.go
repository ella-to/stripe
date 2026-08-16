package stripe

import (
	"context"
	"fmt"

	sgo "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/oauth"
)

// AccountType selects the Connect account type, which controls how much of the
// onboarding and dashboard experience Stripe hosts for you.
type AccountType string

const (
	// AccountExpress: Stripe hosts onboarding and a lightweight dashboard.
	// Good default for most marketplaces/platforms.
	AccountExpress AccountType = "express"
	// AccountStandard: the connected user has a full, standalone Stripe
	// account and dashboard.
	AccountStandard AccountType = "standard"
	// AccountCustom: you build and host the entire experience; Stripe shows
	// nothing to the connected user.
	AccountCustom AccountType = "custom"
)

// ConnectedAccountParams describes a connected account to register. Only Type
// is strictly required; everything else is optional and can be completed later
// through the onboarding link.
type ConnectedAccountParams struct {
	Type         AccountType
	Email        string
	Country      string   // ISO country code, e.g. "US".
	BusinessType string   // "individual" or "company" (optional).
	Capabilities []string // e.g. []string{"card_payments", "transfers"}.
	Metadata     map[string]string
}

// RegisterConnectedAccount creates a new connected account under the platform.
//
// Requested capabilities are sent in the "requested" state; the account must
// finish onboarding (see AccountOnboardingLink) before they become active.
func (c *Client) RegisterConnectedAccount(ctx context.Context, p ConnectedAccountParams) (*Account, error) {
	if p.Type == "" {
		p.Type = AccountExpress
	}

	params := &sgo.AccountCreateParams{
		Type: String(string(p.Type)),
	}
	if p.Email != "" {
		params.Email = String(p.Email)
	}
	if p.Country != "" {
		params.Country = String(p.Country)
	}
	if p.BusinessType != "" {
		params.BusinessType = String(p.BusinessType)
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	// Capabilities have a wide, ever-growing surface; requesting them via
	// AddExtra keeps this wrapper forward compatible with new capability names.
	for _, cap := range p.Capabilities {
		params.AddExtra(fmt.Sprintf("capabilities[%s][requested]", cap), "true")
	}

	c.prep(&params.Params)
	return c.api.V1Accounts.Create(ctx, params)
}

// DeleteConnectedAccount permanently deletes a connected account. This only
// works for accounts the platform controls (Express/Custom) or test accounts.
func (c *Client) DeleteConnectedAccount(ctx context.Context, accountID string) error {
	params := &sgo.AccountDeleteParams{}
	c.prep(&params.Params)
	_, err := c.api.V1Accounts.Delete(ctx, accountID, params)
	return err
}

// ConnectedAccountUpdate carries the mutable fields of a connected account.
// Nil/zero fields are left untouched.
type ConnectedAccountUpdate struct {
	Email    string
	Metadata map[string]string
	// Defaults is an escape hatch for any other account field, expressed as
	// stripe form keys, e.g. {"business_profile[url]": "https://acme.test"}.
	Defaults map[string]string
}

// UpdateConnectedAccount patches an existing connected account.
func (c *Client) UpdateConnectedAccount(ctx context.Context, accountID string, p ConnectedAccountUpdate) (*Account, error) {
	params := &sgo.AccountUpdateParams{}
	if p.Email != "" {
		params.Email = String(p.Email)
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	for k, v := range p.Defaults {
		params.AddExtra(k, v)
	}
	c.prep(&params.Params)
	return c.api.V1Accounts.Update(ctx, accountID, params)
}

// AccountOnboardingLink returns a single-use URL that walks the connected
// account through initial onboarding. Redirect the user to AccountLink.URL.
//
// refreshURL is hit if the link expires before completion; returnURL is hit
// once the user finishes (or backs out of) the flow.
func (c *Client) AccountOnboardingLink(ctx context.Context, accountID, refreshURL, returnURL string) (*AccountLink, error) {
	return c.accountLink(ctx, accountID, refreshURL, returnURL, "account_onboarding")
}

// RequestAccountUpdate returns a single-use URL that lets an already-onboarded
// connected account update previously provided information (the "account_update"
// flow). Use this when Stripe reports newly required information.
func (c *Client) RequestAccountUpdate(ctx context.Context, accountID, refreshURL, returnURL string) (*AccountLink, error) {
	return c.accountLink(ctx, accountID, refreshURL, returnURL, "account_update")
}

func (c *Client) accountLink(ctx context.Context, accountID, refreshURL, returnURL, linkType string) (*AccountLink, error) {
	params := &sgo.AccountLinkCreateParams{
		Account:    String(accountID),
		RefreshURL: String(refreshURL),
		ReturnURL:  String(returnURL),
		Type:       String(linkType),
	}
	return c.api.V1AccountLinks.Create(ctx, params)
}

// ConnectAuthorizeURL builds the URL you redirect an existing Stripe account
// owner to so they can connect their account to your platform via OAuth.
//
// state should be an unguessable value you later verify in the callback.
// Requires WithOAuthClientID and WithOAuthRedirectURI to have been set on the
// client.
func (c *Client) ConnectAuthorizeURL(state string, scopes ...string) (string, error) {
	if c.oauthClientID == "" {
		return "", fmt.Errorf("stripe: ConnectAuthorizeURL requires WithOAuthClientID")
	}
	if c.oauthRedirectURI == "" {
		return "", fmt.Errorf("stripe: ConnectAuthorizeURL requires WithOAuthRedirectURI")
	}
	scope := "read_write"
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	params := &sgo.AuthorizeURLParams{
		ClientID:     String(c.oauthClientID),
		State:        String(state),
		Scope:        String(scope),
		ResponseType: String("code"),
		RedirectURI:  String(c.oauthRedirectURI),
	}
	oc := oauth.Client{B: sgo.GetBackend(sgo.ConnectBackend), Key: c.apiKey}
	return oc.AuthorizeURL(params), nil
}

// ConnectExistingAccount exchanges the OAuth authorization code received on the
// Connect callback for the connected account. The returned account id can then
// be used with ForAccount to act on that account's behalf.
func (c *Client) ConnectExistingAccount(ctx context.Context, code string) (*sgo.OAuthToken, error) {
	params := &sgo.OAuthTokenParams{
		GrantType:    String("authorization_code"),
		Code:         String(code),
		ClientSecret: String(c.apiKey),
	}
	oc := oauth.Client{B: sgo.GetBackend(sgo.ConnectBackend), Key: c.apiKey}
	return oc.New(params)
}

// ClientFromOAuthCode completes the Connect OAuth flow in one step: it exchanges
// the authorization code (using this platform client's secret key) and returns
// a new Client that authenticates as the connected account via its access
// token, along with the raw token (store the refresh token / account id).
//
// The platform secret is only used here, for the handshake; the returned client
// operates entirely with the tenant's access token.
func (c *Client) ClientFromOAuthCode(ctx context.Context, code string, opts ...Option) (*Client, *OAuthToken, error) {
	tok, err := c.ConnectExistingAccount(ctx, code)
	if err != nil {
		return nil, nil, err
	}
	return NewFromOAuthToken(tok, opts...), tok, nil
}

// DisconnectAccount revokes the platform's access to a previously connected
// (OAuth) account.
func (c *Client) DisconnectAccount(ctx context.Context, accountID string) error {
	if c.oauthClientID == "" {
		return fmt.Errorf("stripe: DisconnectAccount requires WithOAuthClientID")
	}
	params := &sgo.DeauthorizeParams{
		ClientID:     String(c.oauthClientID),
		StripeUserID: String(accountID),
	}
	oc := oauth.Client{B: sgo.GetBackend(sgo.ConnectBackend), Key: c.apiKey}
	_, err := oc.Del(params)
	return err
}
