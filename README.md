# stripe — a simpler Stripe SDK for Go

A thin, opinionated wrapper around the official [`stripe-go`](https://github.com/stripe/stripe-go)
SDK (v85). Stripe is powerful but its API surface is large and the common
patterns take a lot of boilerplate to assemble. This package collapses those
patterns into small, task‑oriented calls — while still returning the **real**
stripe-go resource types, so you can always drop down to the full SDK when you
need to.

```go
import "ella.to/stripe"

client := stripe.New(os.Getenv("STRIPE_SECRET_KEY"))
```

## Features

| Area | What you get |
|------|--------------|
| **Connect** | Register / update / delete connected accounts, hosted onboarding & update links, connect an existing account via OAuth |
| **Subscriptions** | Monthly / yearly plans, trials (days or months), plan swaps, cancel now or at period end |
| **Quota / usage** | Usage meters, package pricing with automatic overage, usage reporting, expiring (monthly) credit grants |
| **Purchase** | A cart builder with inline or referenced items, automatic or manual tax, flat‑rate shipping, hosted Checkout |
| **Webhooks** | Create / update / delete endpoints on demand, plus a **generic, type‑safe dispatcher** that validates payloads, casts them to the right Go type and runs handlers in goroutines while acknowledging Stripe instantly |
| **Platform fees** | Per‑connected‑account application fees (percentage and/or fixed), changeable any time, applied to direct charges, Checkout and subscriptions |
| **Tax** | Tax rates per jurisdiction, on the platform or on a connected account |

## Design

- **One import.** Common resource types (`stripe.Subscription`, `stripe.Invoice`,
  `stripe.Account`, …) are re-exported, so handlers and callers only import this
  package.
- **Connected accounts are first class.** `client.ForAccount("acct_123")`
  returns a scoped client; every subsequent call is made on behalf of that
  account via the `Stripe-Account` header.
- **Nothing is hidden.** Every method returns the underlying stripe-go struct,
  and `client.Raw()` exposes the full official client.
- **Money helpers.** `stripe.Dollars(19.99)` → `1999` (minor units).

## Quick reference

### Connect

```go
acct, _ := client.RegisterConnectedAccount(ctx, stripe.ConnectedAccountParams{
    Type:         stripe.AccountExpress,
    Email:        "seller@example.com",
    Country:      "US",
    Capabilities: []string{"card_payments", "transfers"},
})

link, _ := client.AccountOnboardingLink(ctx, acct.ID, reauthURL, returnURL)
// redirect the user to link.URL

upd, _ := client.RequestAccountUpdate(ctx, acct.ID, reauthURL, returnURL)
_       = client.DeleteConnectedAccount(ctx, acct.ID)

// Connect an existing Stripe account via OAuth:
authURL, _ := client.ConnectAuthorizeURL("csrf-state")        // step 1: redirect
token, _   := client.ConnectExistingAccount(ctx, callbackCode) // step 2: callback
connected  := token.StripeUserID                               // acct_...
```

### Subscriptions

```go
monthly, _ := client.CreatePlan(ctx, stripe.PlanParams{
    ProductName: "Pro", Amount: stripe.Dollars(20), Interval: stripe.Monthly,
})

sub, _ := client.Subscribe(ctx, stripe.SubscribeParams{
    Customer: "cus_123", PriceID: monthly.ID, TrialDays: 14,
})
// 3-month trial: TrialEnd: time.Now().AddDate(0, 3, 0)

client.SwapPlan(ctx, sub.ID, yearly.ID)
client.Unsubscribe(ctx, sub.ID, stripe.CancelAtPeriodEnd) // or stripe.CancelImmediately
```

### Quota / usage based billing

"1000 API requests cost $2, every additional 1000 costs another $2":

```go
plan, _ := client.SetupMeteredQuota(ctx, stripe.SetupMeteredQuotaParams{
    ProductName:      "API Requests",
    EventName:        "api_request",
    AmountPerPackage: stripe.Dollars(2),
    PackageSize:      1000,
    Interval:         stripe.Monthly,
})

client.Subscribe(ctx, stripe.SubscribeParams{Customer: "cus_123", PriceID: plan.Price.ID})
client.ReportUsage(ctx, "cus_123", "api_request", 1500) // charged $4 (2 packages)
```

The charge is `ceil(usage / PackageSize) * AmountPerPackage`, so overage is
automatic. For a **prepaid, monthly‑expiring** quota, grant credit that expires
and re-grant it each cycle (e.g. from an `invoice.paid` webhook):

```go
client.GrantQuota(ctx, stripe.QuotaGrantParams{
    Customer:  "cus_123",
    Amount:    stripe.Dollars(2),
    ExpiresIn: 30 * 24 * time.Hour,
    PriceIDs:  []string{plan.Price.ID},
})
```

### Purchase (cart → Checkout)

```go
cart := stripe.NewCart("usd").
    AddItem("T-Shirt", stripe.Dollars(25), 2).
    AddItem("Sticker pack", stripe.Dollars(5), 1).
    WithAutomaticTax().                          // tax added later, at checkout
    AddShipping("Standard", stripe.Dollars(5))   // shipping added later

session, _ := client.Checkout(ctx, stripe.CheckoutParams{
    Cart: cart, SuccessURL: successURL, CancelURL: cancelURL,
})
// redirect the buyer to session.URL
```

### Webhooks

Manage endpoints on demand and dispatch events with **generics** — each handler
receives a fully decoded, type‑checked object. Handlers run in their own
goroutines so Stripe gets its `200` immediately.

```go
client := stripe.New(key, stripe.WithWebhookSecret(os.Getenv("STRIPE_WEBHOOK_SECRET")))

ep, _ := client.CreateWebhookEndpoint(ctx, stripe.WebhookEndpointParams{
    URL:    "https://app.example.com/stripe/webhook",
    Events: []string{"invoice.paid", "customer.subscription.deleted"},
})
// store ep.Secret

d := client.Webhooks(stripe.WithErrorHandler(func(ev stripe.Event, err error) {
    log.Printf("handler error for %s: %v", ev.Type, err)
}))

stripe.On(d, stripe.EventInvoicePaid, func(ctx context.Context, ev stripe.Event, inv *stripe.Invoice) error {
    log.Printf("invoice %s paid: %d", inv.ID, inv.AmountPaid)
    return nil
})

http.Handle("/stripe/webhook", d) // d implements http.Handler
```

`stripe.On[T]` is a package‑level function (Go methods can't take type
parameters). The event JSON is validated by decoding it into `*T`; on mismatch
the error handler fires and the typed handler is skipped.

### Platform fees (SaaS revenue share)

Charge a fee on every connected‑account transaction — a percentage, a fixed
amount, or both. Fees use Stripe **application fees on direct charges**, so the
connected account is the settlement merchant and pays Stripe's processing fee,
while your platform collects its fee separately.

> Example: a $10.00 charge where Stripe's fee is ~$3.00 and your platform fee is
> $3.00 leaves the seller with ~$4.00 and your platform with $3.00.

Each account's fee is stored on the account itself (metadata), so you can change
it at any time without a separate database, and assign different fees to
different accounts:

```go
// Assign / change a fee for an account (effective immediately on new charges).
client.SetPlatformFee(ctx, "acct_seller", stripe.PlatformFee{Fixed: stripe.Dollars(3)})
client.SetPlatformFee(ctx, "acct_other",  stripe.PlatformFee{Percent: 2.9, Fixed: 30})

// One-off charge: fee resolved from the account automatically.
pi, _ := client.ChargeWithFee(ctx, stripe.ChargeParams{
    ConnectedAccount: "acct_seller",
    Amount:           stripe.Dollars(10),
    PaymentMethod:    "pm_card_visa",
    Confirm:          true,
})
// pi.ApplicationFeeAmount == 300  // what your platform earns

// Override the stored fee for a single charge:
client.ChargeWithFee(ctx, stripe.ChargeParams{
    ConnectedAccount: "acct_seller",
    Amount:           stripe.Dollars(50),
    Fee:              &stripe.PlatformFee{Percent: 10},
})
```

The same fee flows through Checkout and subscriptions:

```go
// Hosted Checkout as a direct charge with a platform fee:
client.Checkout(ctx, stripe.CheckoutParams{
    Cart: cart, ConnectedAccount: "acct_seller",
    SuccessURL: successURL, CancelURL: cancelURL,
    // Fee resolved from the account, or set Fee / FeeAmount explicitly.
})

// Recurring revenue share (Stripe supports a *percentage* fee on subscriptions):
client.Subscribe(ctx, stripe.SubscribeParams{
    Customer: "cus_123", PriceID: priceID,
    ConnectedAccount: "acct_seller", FeePercent: 10, // 10% of every invoice
})
```

`PlatformFee.Compute(amount)` returns the fee in minor units so you can preview
the split before charging.

**Where fees are stored is pluggable.** By default they live on each connected
account's Stripe metadata (`AccountMetadataFeeStore`), so there's no extra
database. To source fees from your own store, implement `FeeResolver` and pass
it with `WithFeeResolver`; it is consulted whenever a charge doesn't pass an
explicit fee:

```go
client := stripe.New(key, stripe.WithFeeResolver(stripe.FeeResolverFunc(
    func(ctx context.Context, accountID string) (stripe.PlatformFee, bool, error) {
        row, ok := db.LookupFee(ctx, accountID) // your storage
        if !ok {
            return stripe.PlatformFee{}, false, nil // no fee configured
        }
        return stripe.PlatformFee{Percent: row.Percent, Fixed: row.Fixed}, true, nil
    },
)))
```

### OAuth: drive the SDK as a connected account (multi-tenant SaaS)

Instead of holding each tenant's secret key, let tenants connect their Stripe
account via Connect **OAuth**. The token exchange returns an `access_token` that
is itself a usable API key, so the whole SDK can operate *as* that tenant. Your
meta-platform only needs its own secret key for the one-time handshake.

```go
platform := stripe.New(key, stripe.WithOAuthClientID("ca_..."))

// 1. Redirect the tenant to Stripe.
url, _ := platform.ConnectAuthorizeURL("state")

// 2. In your OAuth callback, exchange the code for a client that acts as the tenant.
tenant, tok, _ := platform.ClientFromOAuthCode(ctx, code)
tenant.AccountID()      // acct_... the token represents
// `tenant` is keyed by tok.AccessToken — every call now runs as that account.

// Later, rebuild a tenant client from a stored token:
tenant := stripe.New(storedAccessToken, stripe.WithFeeResolver(myStore))
```

**Each tenant adjusting their own platform fee** falls out of the `FeeResolver`:
store each tenant's chosen rate in your own database (keyed by their account id)
and have the resolver return it. A tenant changing their fee is just a write to
your store; the SDK applies the new rate on the next charge. See
[`examples/oauthsaas`](./examples/oauthsaas) for the full HTTP flow.

> Note: the `access_token` for a Standard Connect account does not expire, but
> the exchange also returns a `RefreshToken` (exposed via `client.RefreshToken()`)
> should you need it.

### Tax

```go
client.CreateTaxRate(ctx, stripe.TaxRateParams{
    DisplayName: "CA Sales Tax", Percentage: 7.25,
    Country: "US", State: "CA", Jurisdiction: "California", TaxType: "sales_tax",
})

// On a connected account:
client.CreateTaxRateForAccount(ctx, "acct_123", stripe.TaxRateParams{
    DisplayName: "VAT", Percentage: 19, Country: "DE", TaxType: "vat", Inclusive: true,
})
```

## Examples

Runnable programs live in [`examples/`](./examples):

- `examples/connect` — connected accounts + OAuth
- `examples/subscriptions` — plans, trials, cancellation
- `examples/quota` — metered/package pricing and credit grants
- `examples/purchase` — cart, tax, shipping, Checkout
- `examples/webhook` — endpoint management + typed dispatcher HTTP server
- `examples/platformfee` — per‑account platform fees on direct charges
- `examples/oauthsaas` — multi‑tenant OAuth onboarding + per‑tenant fees
- `examples/tax` — jurisdiction tax rates

All read `STRIPE_SECRET_KEY` (and a few feature‑specific env vars) from the
environment.

## Testing

```sh
go test ./...
```

The webhook dispatcher is covered by an offline test that signs a payload with
Stripe's test helper and asserts the typed handler receives the decoded object.

## Requirements

- Go 1.23+ (uses range‑over‑func for list iteration; module targets 1.25)
- `github.com/stripe/stripe-go/v85`
