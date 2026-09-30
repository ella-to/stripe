# 12 · Connect OAuth (multi-tenant SaaS)

Your tenants already have Stripe accounts. They click **Connect**, approve
your platform, and your app then works *as* their account using an access
token. No secret keys are pasted anywhere.

```text
/connect ──▶ Stripe (approve) ──▶ /callback?code=...&state=... ──▶ access token stored
/tenant/{id} ──▶ change own fee · act as the tenant · disconnect
```

> New platforms may be steered towards Express accounts and Account Links
> instead of OAuth; see [11 · Marketplace](../11-marketplace).

## Run it

**0. Set up OAuth** (once, test mode): Dashboard → **Connect** → **Settings** →
**OAuth**: enable OAuth, add the redirect URI `http://localhost:4242/callback`,
and copy the **Client ID** (`ca_...`).

```sh
# terminal 1
stripe listen --forward-to localhost:4242/webhook --forward-connect-to localhost:4242/webhook

# terminal 2
export STRIPE_SECRET_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)
export STRIPE_CONNECT_CLIENT_ID=ca_...
go run ./guides/12-connect-oauth
```

Open <http://localhost:4242>, click **Connect your Stripe account**. In test
mode Stripe offers **Skip this form**, which creates a test account and
connects it straight away.

## The code, step by step

**1. The platform client**

```go
platform := stripe.New(os.Getenv("STRIPE_SECRET_KEY"),
    stripe.WithOAuthClientID(os.Getenv("STRIPE_CONNECT_CLIENT_ID")),
    stripe.WithOAuthRedirectURI("http://localhost:4242/callback"),
    stripe.WithFeeResolver(db), // tenants' fees come from your database
)
```

**2. Redirect to Stripe with a random state**

```go
state := randomString()
http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: state, HttpOnly: true})
url, _ := platform.ConnectAuthorizeURL(state)
http.Redirect(w, r, url, http.StatusFound)
```

**3. Callback: check the state, exchange the code**

```go
if cookie.Value != r.URL.Query().Get("state") { /* reject */ }

tenantClient, tok, err := platform.ClientFromOAuthCode(ctx, r.URL.Query().Get("code"))
db.save(&tenant{AccountID: tenantClient.AccountID(), AccessToken: tok.AccessToken, FeePercent: 5})
```

**4. Act as the tenant later**

```go
tenantClient := stripe.New(t.AccessToken, stripe.WithFeeResolver(db))
tenantClient.CreateCustomer(ctx, stripe.CreateCustomerParams{Email: "..."}) // created on their account
```

**5. Tenants set their own fee** — it's just a row in your store:

```go
func (s *store) ResolveFee(ctx context.Context, accountID string) (stripe.PlatformFee, bool, error) {
    t, ok := s.get(accountID)
    return stripe.PlatformFee{Percent: t.FeePercent}, ok, nil
}
// ChargeWithFee / Checkout / Subscribe with ConnectedAccount use it automatically.
```

**6. Disconnect** (from your side, or react when they do it from theirs)

```go
platform.DisconnectAccount(ctx, accountID)

stripe.On(hooks, stripe.EventAccountApplicationDeauthorized, func(ctx context.Context, ev stripe.Event, _ *struct{}) error {
    db.remove(ev.Account)
    return nil
})
```

## Test it

| Try                                     | Result                                           |
| --------------------------------------- | ------------------------------------------------ |
| Connect → Skip this form                | Tenant page opens, fee 5%                        |
| Change the fee to 12.5                  | Log: `fee updated percent=12.5`                  |
| Create a customer on this account       | Customer shows up in the tenant's dashboard      |
| Open `/callback?code=x&state=wrong`     | `invalid state`                                  |
| Disconnect                              | Tenant removed; `account.application.deauthorized` arrives |

## Next

→ Back to the [guides index](../README.md)
