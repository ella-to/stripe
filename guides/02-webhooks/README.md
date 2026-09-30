# 02 · Webhooks

Stripe tells your server what happened (payment succeeded, subscription
cancelled, ...) by POSTing **events** to a webhook endpoint. The dispatcher
verifies the signature, decodes each event into the right Go type and calls
your handler.

```text
Stripe ──POST /webhook (signed)──▶ Dispatcher ──▶ stripe.On(hooks, EventInvoicePaid, func(..., inv *stripe.Invoice))
```

## Run it

```sh
# terminal 1
stripe listen --forward-to localhost:4242/webhook

# terminal 2
export STRIPE_SECRET_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)
go run ./guides/02-webhooks

# terminal 3
stripe trigger payment_intent.succeeded
```

```text
INFO payment succeeded payment_intent=pi_... amount=2000 currency=usd
```

## The code, step by step

**1. Create the dispatcher**

```go
client := stripe.New(os.Getenv("STRIPE_SECRET_KEY"),
    stripe.WithWebhookSecret(os.Getenv("STRIPE_WEBHOOK_SECRET")),
    stripe.WithLogger(slog.Default()), // errors are logged with slog (the default)
)

hooks := client.Webhooks(
    stripe.WithIgnoreAPIVersionMismatch(), // for `stripe listen`
)
```

**2. Register typed handlers**

```go
stripe.On(hooks, stripe.EventInvoicePaid, func(ctx context.Context, ev stripe.Event, inv *stripe.Invoice) error {
    slog.Info("invoice paid", "invoice", inv.ID, "amount_paid", inv.AmountPaid)
    return nil
})

stripe.On(hooks, stripe.EventCustomerSubscriptionUpdated, func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
    // save sub.Status / stripe.HasAccess(sub) on your user
    return nil
})
```

**3. Make handlers idempotent** (Stripe can deliver the same event twice)

```go
stripe.On(hooks, stripe.EventInvoicePaid, func(ctx context.Context, ev stripe.Event, inv *stripe.Invoice) error {
    if !firstTime(ev) { // remembers ev.ID; use a DB table in production
        return nil
    }
    // ...
})
```

**4. Mount it and shut down gracefully**

```go
mux.Handle("POST /webhook", hooks)
// ... on SIGTERM:
srv.Shutdown(ctx)
hooks.Wait() // wait for handlers still running
```

**Async vs sync.** By default Stripe gets its `200` right away and handlers run
in goroutines. With `stripe.WithSyncHandlers()` handlers run first and an error
returns `500`, so Stripe retries the event later.

## Test it

```sh
stripe trigger checkout.session.completed
stripe trigger customer.subscription.created
stripe trigger customer.subscription.updated
stripe trigger customer.subscription.deleted
stripe trigger invoice.paid
stripe trigger invoice.payment_failed
stripe trigger payment_intent.succeeded

# replay an event: the second delivery is skipped as a duplicate
stripe events resend evt_...

# a bad signature is rejected with 400 and logged
curl -X POST localhost:4242/webhook -H 'Stripe-Signature: t=1,v1=bad' -d '{}'
```

`stripe trigger` creates real test objects, so one trigger fires several events
(e.g. `invoice.paid` also sends `payment_intent.succeeded`).

## Production

`stripe listen` is for local testing. For a deployed server, register the
endpoint once and store its signing secret:

```go
ep, err := client.CreateWebhookEndpoint(ctx, stripe.WebhookEndpointParams{
    URL:    "https://example.com/webhook",
    Events: []string{"checkout.session.completed", "customer.subscription.updated", "invoice.paid"},
})
// save ep.Secret (whsec_...) as STRIPE_WEBHOOK_SECRET
```

`CreateWebhookEndpoint` pins the endpoint to the SDK's API version, so you can
drop `WithIgnoreAPIVersionMismatch()` in production.

Using Connect? Events from connected accounts need an extra flag locally:

```sh
stripe listen --forward-to localhost:4242/webhook --forward-connect-to localhost:4242/webhook
```

## Next

→ [03 · One-time payment](../03-one-time-payment)
