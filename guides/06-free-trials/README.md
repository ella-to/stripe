# 06 · Free trials

A 14-day trial, two ways: **card required** (converts automatically) or
**no card** (cancels at the end unless the customer adds one). Plus the
"your trial ends soon" reminder.

```text
/  ──POST /trial?card=yes|no──▶ Checkout ──▶ status: trialing
                                          ├─ 3 days before end: customer.subscription.trial_will_end ──▶ reminder email
                                          └─ trial ends: active (card) or canceled (no card)
```

## Run it

```sh
# terminal 1
stripe listen --forward-to localhost:4242/webhook

# terminal 2
export STRIPE_SECRET_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)
go run ./guides/06-free-trials
```

Open <http://localhost:4242> and start either trial. `/account` shows
**trialing**, access **true**, and the trial end date.

## The code, step by step

**1. Trial with a card** (charged automatically when the trial ends)

```go
session, err := client.CheckoutSubscription(ctx, stripe.SubscriptionCheckoutParams{
    PriceID:    plan.ID,
    Customer:   user.CustomerID,
    TrialDays:  14,
    SuccessURL: "http://localhost:4242/account",
    CancelURL:  "http://localhost:4242/",
})
```

**2. Trial without a card** (only an email is collected)

```go
session, err := client.CheckoutSubscription(ctx, stripe.SubscriptionCheckoutParams{
    PriceID:          plan.ID,
    Customer:         user.CustomerID,
    TrialDays:        14,
    TrialWithoutCard: true, // no card at the end → subscription is canceled
    SuccessURL:       "http://localhost:4242/account",
    CancelURL:        "http://localhost:4242/",
})
```

**3. Remind before the trial ends**

```go
stripe.On(hooks, stripe.EventCustomerSubscriptionTrialWillEnd, func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
    sendEmail(sub.Metadata["user_id"], "Your trial ends on "+time.Unix(sub.TrialEnd, 0).Format(time.DateOnly))
    return nil
})
```

**4. Track the outcome**

```go
save := func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
    db.SaveSubscription(sub) // trialing → active, or → canceled
    return nil
}
stripe.On(hooks, stripe.EventCustomerSubscriptionUpdated, save)
stripe.On(hooks, stripe.EventCustomerSubscriptionDeleted, save)

stripe.HasAccess(sub)               // true while trialing
stripe.SubscriptionAccessUntil(sub) // the trial end date while trialing
```

## Test it

| Try                      | How                                                     | Result                                  |
| ------------------------ | ------------------------------------------------------- | --------------------------------------- |
| Trial with card          | "card required" + `4242 4242 4242 4242`                 | `trialing`, card on file: `true`        |
| Trial without card       | "no card", enter only an email                          | `trialing`, card on file: `false`       |
| Reminder email           | `stripe trigger customer.subscription.trial_will_end`   | reminder logged (unknown customer is expected) |
| End the trial now        | `stripe subscriptions update sub_... -d trial_end=now` | card → `active`; no card → `canceled`   |

**Fast-forward time with test clocks.** To watch a full 14-day trial play out
(reminder → conversion or cancellation), create a customer on a *test clock* in
the Dashboard: **Billing → Test clocks**, add a customer and subscription, then
advance the clock. The webhooks fire as if the days had passed.

## Next

→ [07 · Manage subscriptions](../07-manage-subscriptions)
