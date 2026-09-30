# stripe — a simpler Stripe SDK for Go

A thin, opinionated wrapper around the official
[`stripe-go`](https://github.com/stripe/stripe-go) SDK (v86, API version
`2026-08-26.dahlia`). The common Stripe patterns — Checkout, subscriptions,
trials, the billing portal, usage-based billing, refunds, webhooks, tax and
Connect marketplaces — become small, task-oriented calls. Every method returns
the **real** stripe-go types, so you can always drop down to the full SDK.

```sh
go get ella.to/stripe
```

```go
client := stripe.New(os.Getenv("STRIPE_SECRET_KEY"))

session, err := client.Checkout(ctx, stripe.CheckoutParams{
    Cart:       stripe.NewCart("usd").AddItem("T-Shirt", stripe.Dollars(25), 1),
    SuccessURL: "https://example.com/success?session_id={CHECKOUT_SESSION_ID}",
    CancelURL:  "https://example.com/",
})
// redirect the buyer to session.URL
```

## 📚 Guides

Step-by-step recipes with code that runs out of the box in test mode, including
how to test locally with the Stripe CLI → **[guides/](./guides)**

| Guide                                                         | Guide                                                            |
| ------------------------------------------------------------- | ---------------------------------------------------------------- |
| [00 · Setup](./guides/00-setup)                               | [07 · Manage subscriptions](./guides/07-manage-subscriptions)    |
| [01 · Customers](./guides/01-customers)                       | [08 · Refunds](./guides/08-refunds)                              |
| [02 · Webhooks](./guides/02-webhooks)                         | [09 · Usage-based billing](./guides/09-usage-based-billing)      |
| [03 · One-time payment](./guides/03-one-time-payment)         | [10 · Tax](./guides/10-tax)                                      |
| [04 · Shopping cart](./guides/04-shopping-cart)               | [11 · Marketplace (Connect)](./guides/11-marketplace)            |
| [05 · Subscriptions](./guides/05-subscriptions)               | [12 · Connect OAuth](./guides/12-connect-oauth)                  |
| [06 · Free trials](./guides/06-free-trials)                   |                                                                  |

## Features

| Area              | What you get                                                                                                    |
| ----------------- | --------------------------------------------------------------------------------------------------------------- |
| **Customers**     | Create / get / update / delete, find by email, attach a saved card                                              |
| **Payments**      | Cart builder → hosted Checkout (shipping, address collection, promo codes, tax), charge a saved card, refunds  |
| **Subscriptions** | Idempotent plans (lookup keys), subscribe via Checkout or API, trials with/without card, coupons, plan swaps, cancel / resume, cancel with refund, `HasAccess`, upcoming invoice |
| **Billing portal**| One call to open the hosted portal; create a portal configuration from code                                     |
| **Usage billing** | Meters, package pricing with automatic overage, idempotent usage events, expiring credit grants                 |
| **Webhooks**      | Generic, type-safe dispatcher: verifies signatures, decodes into the right Go type, async or sync handlers, slog logging, forwarding; endpoint management |
| **Connect**       | Express onboarding, account readiness, Express dashboard links, OAuth for existing accounts, per-account platform fees on direct charges, Checkout and subscriptions |
| **Tax**           | Tax rates per jurisdiction (platform or connected account), Stripe Tax at Checkout                              |

## Quick reference

```go
client := stripe.New(os.Getenv("STRIPE_SECRET_KEY"),
    stripe.WithWebhookSecret(os.Getenv("STRIPE_WEBHOOK_SECRET")),
    stripe.WithLogger(slog.Default()), // optional: all logging goes through log/slog
)
```

### Customers

```go
cus, _ := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
    Email:         "alice@example.com",
    Metadata:      map[string]string{"app_user_id": "u-42"},
    PaymentMethod: "pm_card_visa", // optional: saved card (test token here)
})
cus, found, _ := client.FindCustomerByEmail(ctx, "alice@example.com")
cus, _ = client.UpdateCustomer(ctx, cus.ID, stripe.UpdateCustomerParams{Name: "Alice"})
client.DeleteCustomer(ctx, cus.ID)
```

### One-time payments

```go
cart := stripe.NewCart("usd").
    AddItem("T-Shirt", stripe.Dollars(25), 2).
    AddShipping("Standard", stripe.Dollars(5)).
    ShipTo("US", "CA")

session, _ := client.Checkout(ctx, stripe.CheckoutParams{
    Cart:                cart,
    ClientReferenceID:   orderID,
    AllowPromotionCodes: true,
    SuccessURL:          "https://example.com/success?session_id={CHECKOUT_SESSION_ID}",
    CancelURL:           "https://example.com/cart",
})

cs, _ := client.GetCheckoutSession(ctx, sessionID) // success page: line items, payment status

// Charge a saved card without the customer present.
pi, _ := client.ChargeCustomer(ctx, stripe.ChargeCustomerParams{
    Customer: cus.ID, Amount: stripe.Dollars(9.99), IdempotencyKey: "topup-123",
})

// Refunds: Amount 0 = full refund.
client.RefundPayment(ctx, stripe.RefundParams{PaymentIntentID: pi.ID, Amount: stripe.Dollars(5)})
```

### Subscriptions

```go
// Safe to run on every start: found by lookup key, created only once.
monthly, _ := client.EnsurePlan(ctx, stripe.PlanParams{
    LookupKey: "pro_monthly", ProductName: "Pro", Amount: stripe.Dollars(20), Interval: stripe.Monthly,
})
yearly, _ := client.EnsurePlan(ctx, stripe.PlanParams{
    LookupKey: "pro_yearly", ProductID: monthly.Product.ID, Amount: stripe.Dollars(200), Interval: stripe.Yearly,
})

// Sign up through hosted Checkout (recommended)...
session, _ := client.CheckoutSubscription(ctx, stripe.SubscriptionCheckoutParams{
    PriceID: monthly.ID, Customer: cus.ID, TrialDays: 14,
    SuccessURL: successURL, CancelURL: cancelURL,
})
// ...or directly, when the customer already has a saved card.
sub, _ := client.Subscribe(ctx, stripe.SubscribeParams{Customer: cus.ID, PriceID: monthly.ID})

stripe.HasAccess(sub)                // active or trialing?
stripe.SubscriptionAccessUntil(sub)  // when access ends
client.SwapPlan(ctx, sub.ID, yearly.ID)
client.Unsubscribe(ctx, sub.ID, stripe.CancelAtPeriodEnd)
client.Resubscribe(ctx, sub.ID)      // undo a pending cancellation
client.UnsubscribeWithRefund(ctx, sub.ID) // cancel now + full refund of the last invoice
client.UpcomingInvoice(ctx, sub.ID)  // what the next bill looks like

// Hosted customer portal: update card, invoices, switch plans, cancel.
cfg, _ := client.CreatePortalConfiguration(ctx, stripe.PortalConfigParams{
    AllowCancel: true, AllowUpdatePaymentMethod: true, AllowInvoiceHistory: true,
    SwitchPrices: []string{monthly.ID, yearly.ID},
})
portal, _ := client.CustomerPortal(ctx, stripe.PortalParams{
    Customer: cus.ID, ReturnURL: "https://example.com/account", ConfigurationID: cfg.ID,
})
// redirect to portal.URL

coupon, _ := client.CreateCoupon(ctx, stripe.CouponParams{PercentOff: 20, Duration: stripe.CouponOnce})
```

### Usage-based billing

"1,000 API requests cost $2; every additional 1,000 costs another $2":

```go
plan, _ := client.SetupMeteredQuota(ctx, stripe.SetupMeteredQuotaParams{
    ProductName: "API Requests", EventName: "api_request", LookupKey: "api_requests",
    AmountPerPackage: stripe.Dollars(2), PackageSize: 1000,
})
client.Subscribe(ctx, stripe.SubscribeParams{Customer: cus.ID, PriceID: plan.Price.ID})

client.ReportUsageEvent(ctx, stripe.UsageEvent{
    Customer: cus.ID, EventName: "api_request", Value: 1, Identifier: requestID, // retries are deduplicated
})

// Prepaid credit that expires (re-grant from an invoice.paid webhook to renew).
grant, _ := client.GrantQuota(ctx, stripe.QuotaGrantParams{
    Customer: cus.ID, Amount: stripe.Dollars(2), ExpiresIn: 30 * 24 * time.Hour,
})
client.VoidCreditGrant(ctx, grant.ID)
```

The charge is `ceil(usage / PackageSize) * AmountPerPackage`. `SetupMeteredQuota`
reuses the meter (by event name) and the price (by lookup key) when they exist.

### Webhooks

```go
hooks := client.Webhooks(
    // Errors are logged with the client's slog logger (stripe.WithLogger, default slog.Default()).
    // stripe.WithSyncHandlers(),      // run handlers before replying; failures → 500 → Stripe retries
    // stripe.WithIgnoreAPIVersionMismatch(), // accept events from `stripe listen` in any API version
)

stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
    return fulfil(cs.ClientReferenceID)
})
stripe.On(hooks, stripe.EventCustomerSubscriptionUpdated, func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
    return saveStatus(sub.Customer.ID, sub.Status)
})

http.Handle("POST /webhook", hooks)
defer hooks.Wait() // on shutdown, let in-flight handlers finish
```

- `stripe.On[T]` is a package-level function (Go methods can't take type
  parameters). The payload is decoded into `*T`; on mismatch the error handler
  fires and the handler is skipped.
- By default handlers run in goroutines and Stripe gets its `200` immediately.
  With `WithSyncHandlers` a failing handler turns into a `500`, so Stripe retries.
- Rejected deliveries (bad signature, API version mismatch, missing secret) are
  reported with the actual reason, both in the response and in the log.
- `client.CreateWebhookEndpoint` pins new endpoints to the SDK's API version, so
  their events always pass verification. `WithForwardURL` relays verified raw
  payloads to another service.

### Connect marketplace and platform fees

```go
acct, _ := client.RegisterConnectedAccount(ctx, stripe.ConnectedAccountParams{
    Type: stripe.AccountExpress, Email: "seller@example.com", Country: "US",
    Capabilities: []string{"card_payments", "transfers"},
})
link, _ := client.AccountOnboardingLink(ctx, acct.ID, refreshURL, returnURL) // redirect to link.URL

acct, _ = client.GetConnectedAccount(ctx, acct.ID)
stripe.AccountReady(acct)                         // finished onboarding, can take payments?
dash, _ := client.ExpressDashboardLink(ctx, acct.ID) // seller's payouts dashboard

// Platform fee per seller (stored on the account's metadata), changeable any time.
client.SetPlatformFee(ctx, acct.ID, stripe.PlatformFee{Percent: 10, Fixed: stripe.Dollars(0.30)})

// Direct charge on the seller; the fee is resolved automatically.
client.Checkout(ctx, stripe.CheckoutParams{Cart: cart, ConnectedAccount: acct.ID, SuccessURL: s, CancelURL: c})
client.CheckoutSubscription(ctx, stripe.SubscriptionCheckoutParams{PriceID: p, ConnectedAccount: acct.ID, SuccessURL: s, CancelURL: c})
client.ChargeWithFee(ctx, stripe.ChargeParams{ConnectedAccount: acct.ID, Amount: stripe.Dollars(10)})
client.RefundPayment(ctx, stripe.RefundParams{PaymentIntentID: pi, ConnectedAccount: acct.ID, RefundApplicationFee: true})

// Act as the connected account for anything else.
client.ForAccount(acct.ID).CreateTaxRate(ctx, stripe.TaxRateParams{DisplayName: "VAT", Percentage: 19})
```

Fees are collected as **application fees on direct charges**: the seller is the
merchant of record and pays Stripe's processing fee, and your platform receives
the fee separately. `PlatformFee.Compute(amount)` previews the split. To keep
fees in your own database, implement `stripe.FeeResolver` and pass it with
`stripe.WithFeeResolver`.

**Existing Stripe accounts via OAuth** (multi-tenant SaaS):

```go
platform := stripe.New(key,
    stripe.WithOAuthClientID("ca_..."),
    stripe.WithOAuthRedirectURI("https://app.example.com/callback"),
)
authURL, _ := platform.ConnectAuthorizeURL(state)            // 1. redirect the tenant
tenant, tok, _ := platform.ClientFromOAuthCode(ctx, code)    // 2. callback: a client acting as the tenant
tenant = stripe.New(tok.AccessToken)                         // later, from the stored token
platform.DisconnectAccount(ctx, tok.StripeUserID)
```

### Tax

```go
rate, _ := client.CreateTaxRate(ctx, stripe.TaxRateParams{
    DisplayName: "CA Sales Tax", Percentage: 7.25, Country: "US", State: "CA", TaxType: "sales_tax",
})
cart.WithTaxRates(rate.ID)  // manual rates on every line
cart.WithAutomaticTax()     // or Stripe Tax (activate it in the Dashboard first)
```

## Testing

**Locally with the Stripe CLI** (real test-mode API + webhooks):

```sh
stripe listen --forward-to localhost:4242/webhook
stripe trigger checkout.session.completed
```

See [guides/00-setup](./guides/00-setup) for the full loop.

**Unit tests** run offline:

```sh
go test ./...
```

**Against [stripe-mock](https://github.com/stripe/stripe-mock)**: every wrapper
method is sent to stripe-mock, which validates the parameters against Stripe's
OpenAPI spec:

```sh
go install github.com/stripe/stripe-mock@latest && stripe-mock &
STRIPE_MOCK_URL=http://localhost:12111 go test ./...
```

In your own tests, point the client at any backend with `WithStripeClient`:

```go
backends := sgo.NewBackendsWithConfig(&sgo.BackendConfig{URL: sgo.String("http://localhost:12111")})
client := stripe.New("sk_test_123", stripe.WithStripeClient(sgo.NewClient("sk_test_123", sgo.WithBackends(backends))))
```

## Examples

[`examples/`](./examples) contains short API tours (`go run ./examples/<name>`),
one per area: `customers`, `purchase`, `subscriptions`, `quota`, `refund`,
`tax`, `webhook`, `connect`, `platformfee`, `oauthsaas`, and `saas` (everything
for a single-account SaaS). For complete, step-by-step flows, see the
[guides](./guides).

## Design

- **One import.** Common resource types (`stripe.Subscription`,
  `stripe.Invoice`, `stripe.Account`, …) and event types are re-exported.
- **Nothing is hidden.** Methods return stripe-go structs; `client.Raw()`
  exposes the full official client.
- **Connected accounts are first class.** `client.ForAccount("acct_123")` scopes
  every call via the `Stripe-Account` header.
- **Safe to re-run.** `EnsurePlan` and `SetupMeteredQuota` find existing objects
  instead of duplicating them.
- **Money helpers.** `stripe.Dollars(19.99)` → `1999` (minor units).

## Requirements

- Go 1.25+
- `github.com/stripe/stripe-go/v86` v86.4.2
