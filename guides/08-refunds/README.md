# 08 · Refunds

Charge a saved card, give part of it back, then refund the rest. Plus: refund
a Checkout purchase.

```text
customer + card ──▶ charge $50 ──▶ refund $20 ──▶ refund remaining $30
```

## Run it

```sh
export STRIPE_SECRET_KEY=sk_test_...
go run ./guides/08-refunds
```

```text
INFO created customer id=cus_...
INFO charged payment_intent=pi_... amount=5000 status=succeeded
INFO partial refund id=re_... amount=2000 status=succeeded
INFO refunded the rest id=re_... amount=3000 status=succeeded
```

Refund a purchase from [03 · One-time payment](../03-one-time-payment):

```sh
go run ./guides/08-refunds cs_test_...   # the session_id from the success page
```

## The code, step by step

**1. A customer with a saved card** (`pm_card_visa` is a test card)

```go
cus, err := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
    Email:         "refunds@example.com",
    PaymentMethod: "pm_card_visa",
})
```

**2. Charge it** (retrying with the same idempotency key never charges twice)

```go
pi, err := client.ChargeCustomer(ctx, stripe.ChargeCustomerParams{
    Customer:       cus.ID,
    Amount:         stripe.Dollars(50),
    IdempotencyKey: orderID,
})
```

**3. Partial refund**

```go
client.RefundPayment(ctx, stripe.RefundParams{
    PaymentIntentID: pi.ID,
    Amount:          stripe.Dollars(20),
    Reason:          stripe.RefundRequestedByCustomer,
})
```

**4. Refund the rest** (`Amount` 0 = everything not yet refunded)

```go
client.RefundPayment(ctx, stripe.RefundParams{PaymentIntentID: pi.ID})
```

**Refund a Checkout purchase**

```go
cs, _ := client.GetCheckoutSession(ctx, sessionID)
client.RefundPayment(ctx, stripe.RefundParams{PaymentIntentID: cs.PaymentIntent.ID})
```

Reasons: `stripe.RefundRequestedByCustomer`, `stripe.RefundDuplicate`,
`stripe.RefundFraudulent` (this one also helps Stripe's fraud models).

**Hear about refunds** (including ones made in the Dashboard)

```go
stripe.On(hooks, stripe.EventChargeRefunded, func(ctx context.Context, ev stripe.Event, ch *stripe.Charge) error {
    slog.Info("refunded", "payment_intent", ch.PaymentIntent.ID, "amount_refunded", ch.AmountRefunded, "fully", ch.Refunded)
    return nil
})
stripe.On(hooks, stripe.EventRefundUpdated, func(ctx context.Context, ev stripe.Event, re *stripe.Refund) error {
    slog.Info("refund status", "id", re.ID, "status", re.Status) // e.g. pending → succeeded / failed
    return nil
})
```

## Test it

```sh
stripe listen --forward-to localhost:4242/webhook   # if you add the handlers above
stripe trigger charge.refunded

# refunds from the CLI
stripe refunds create --payment-intent=pi_... --amount=500
stripe refunds list --limit 3
```

| Try                               | Result                                   |
| --------------------------------- | ---------------------------------------- |
| Refund more than what's left      | `invalid_request_error`: amount too large |
| Charge card `pm_card_chargeDeclined` | `ChargeCustomer` returns a `card_declined` error |

## Next

→ [09 · Usage-based billing](../09-usage-based-billing)
