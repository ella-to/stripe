# 07 · Manage subscriptions

Everything you do to a subscription *after* sign-up, from your own server:
upgrade, preview the next bill, discount, cancel, resume, refund.

Prefer no code? The [customer portal from guide 05](../05-subscriptions) does
plan switching and cancellation for you. Use these calls when your own UI or
admin tools need to do it.

## Run it

```sh
export STRIPE_SECRET_KEY=sk_test_...
go run ./guides/07-manage-subscriptions
```

```text
INFO 1. customer with card customer=cus_...
INFO 2. plans basic=price_... pro=price_... pro_yearly=price_...
INFO 3. subscribed to basic subscription=sub_... status=active
INFO 4. next invoice amount_due=$10.00
INFO 5. upgraded to pro subscription=sub_... next_invoice=$30.00
INFO 6. yearly with coupon subscription=sub_... coupon=...
INFO 7. cancels at period end status=active has_access=true access_until=...
INFO 8. resumed status=active cancel_at_period_end=false
INFO 9. canceled with refund status=canceled refund=re_... amount=$160.00
INFO 10. canceled now status=canceled has_access=false
...
INFO ✅ done — see the customer in the Dashboard url=https://dashboard.stripe.com/test/customers/cus_...
```

(Exact amounts depend on proration timing.)

## The code, step by step

**1. A customer who can be billed without a checkout page**

```go
cus, _ := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
    Email:         "manage@example.com",
    PaymentMethod: "pm_card_visa", // test card; in production it comes from Checkout
})
sub, _ := client.Subscribe(ctx, stripe.SubscribeParams{Customer: cus.ID, PriceID: basic.ID})
```

**2. Preview the next invoice**

```go
next, _ := client.UpcomingInvoice(ctx, sub.ID)
next.AmountDue // cents, includes proration and usage so far
```

**3. Upgrade / downgrade** (prorated automatically)

```go
sub, _ = client.SwapPlan(ctx, sub.ID, pro.ID)
```

**4. Discounts**

```go
coupon, _ := client.CreateCoupon(ctx, stripe.CouponParams{
    Name: "Yearly 20% off", PercentOff: 20, Duration: stripe.CouponOnce,
})
client.Subscribe(ctx, stripe.SubscribeParams{Customer: cus.ID, PriceID: proYearly.ID, CouponID: coupon.ID})
client.DeleteCoupon(ctx, coupon.ID) // stop new redemptions
```

**5. Cancel, resume, refund**

```go
// keep access until the paid period ends
sub, _ = client.Unsubscribe(ctx, sub.ID, stripe.CancelAtPeriodEnd)
stripe.HasAccess(sub)               // still true
stripe.SubscriptionAccessUntil(sub) // the cancel date

// undo it before the period ends
sub, _ = client.Resubscribe(ctx, sub.ID)

// money-back guarantee: cancel now + full refund of the last payment
sub, refund, _ := client.UnsubscribeWithRefund(ctx, sub.ID) // refund is nil if nothing was paid

// cancel now, no refund
sub, _ = client.Unsubscribe(ctx, sub.ID, stripe.CancelImmediately)
```

**6. List a customer's subscriptions**

```go
subs, _ := client.ListSubscriptions(ctx, cus.ID)
```

## Test it

Watch it happen while the guide runs:

```sh
stripe logs tail                                  # every API call the guide makes
stripe listen --events customer.subscription.updated,customer.subscription.deleted,charge.refunded
```

Try a card that attaches but is declined when charged: change
`PaymentMethod` to `pm_card_chargeCustomerFail`. `Subscribe` then returns a
subscription with status `incomplete` instead of `active`.

## Next

→ [08 · Refunds](../08-refunds)
