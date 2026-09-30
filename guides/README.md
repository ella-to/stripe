# Guides

Step-by-step, copy-pasteable recipes for the things almost every website or
service needs from Stripe. Each guide is a folder with a `README.md` (the steps)
and a `main.go` that runs as-is in **test mode**.

| #   | Guide                                                  | You'll build                                                    |
| --- | ------------------------------------------------------ | --------------------------------------------------------------- |
| 00  | [Setup](./00-setup)                                    | Test key, Stripe CLI, the local webhook loop                    |
| 01  | [Customers](./01-customers)                            | Link your users to Stripe customers                             |
| 02  | [Webhooks](./02-webhooks)                              | A verified, typed webhook endpoint + `stripe trigger`           |
| 03  | [One-time payment](./03-one-time-payment)              | Buy button → Checkout → order fulfilled by webhook              |
| 04  | [Shopping cart](./04-shopping-cart)                    | Multi-item cart, shipping, address, promo codes                 |
| 05  | [Subscriptions](./05-subscriptions)                    | Pricing page, subscribe via Checkout, gate features, billing portal |
| 06  | [Free trials](./06-free-trials)                        | Trials with and without a card, trial-ending reminders          |
| 07  | [Manage subscriptions](./07-manage-subscriptions)      | Upgrade, downgrade, cancel, resume, coupons, next invoice       |
| 08  | [Refunds](./08-refunds)                                | Charge a saved card, full and partial refunds                   |
| 09  | [Usage-based billing](./09-usage-based-billing)        | Metered API calls, overage, prepaid credits                     |
| 10  | [Tax](./10-tax)                                        | Tax rates at checkout, Stripe Tax                               |
| 11  | [Marketplace (Connect)](./11-marketplace)              | Seller onboarding, payments with a platform fee, payouts        |
| 12  | [Connect OAuth (multi-tenant)](./12-connect-oauth)     | Tenants connect their own Stripe account                        |

## Before you start

Do [00 · Setup](./00-setup) once. After that every guide is:

```sh
# terminal 1: forward Stripe events to the guide
stripe listen --forward-to localhost:4242/webhook

# terminal 2: run the guide
export STRIPE_SECRET_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)
go run ./guides/<guide>
```

Web guides listen on <http://localhost:4242> and receive webhooks on `/webhook`.
Data lives in memory, so restarting a guide starts fresh.

## Test cards

Any future expiry date, any CVC, any postal code.

| Card                  | Behaviour                         |
| --------------------- | --------------------------------- |
| `4242 4242 4242 4242` | Succeeds                          |
| `4000 0025 0000 3155` | Requires 3-D Secure               |
| `4000 0000 0000 0002` | Declined                          |
| `4000 0000 0000 9995` | Declined: insufficient funds      |
| `pm_card_visa`        | Server-side test payment method   |

More: <https://docs.stripe.com/testing>

## Handy Stripe CLI commands

```sh
stripe trigger checkout.session.completed    # send a realistic test event
stripe trigger --help                        # list of events you can trigger
stripe events resend evt_...                 # replay an event
stripe logs tail                             # live API request log
stripe customers list --limit 3              # any API call from the terminal
```

## Troubleshooting

| Symptom                                    | Fix                                                                          |
| ------------------------------------------ | ---------------------------------------------------------------------------- |
| `missing environment variable`             | Export `STRIPE_SECRET_KEY` / `STRIPE_WEBHOOK_SECRET` in the same terminal    |
| `rejected webhook: ... no valid signature` | The secret changed: re-run `export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)` |
| `rejected webhook: ... API version`        | The guides already pass `stripe.WithIgnoreAPIVersionMismatch()`; in production create the endpoint with `client.CreateWebhookEndpoint` (it pins the SDK's version) |
| `webhook for unknown order/user`           | Expected for `stripe trigger` events: they aren't tied to your in-memory data |
| Nothing arrives at `/webhook`              | Is `stripe listen` running? Is the guide on `:4242`?                         |
