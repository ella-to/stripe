# 05 · Subscriptions

A pricing page (monthly / yearly) → Stripe Checkout → subscription synced by
webhooks → a feature gated on it → a **Manage billing** button (customer portal).

```text
/  ──POST /subscribe──▶ Checkout (subscription) ──▶ /account
                              │
                              └── customer.subscription.* ──▶ /webhook ──▶ user.Sub
/pro-feature  ── stripe.HasAccess(user.Sub) ? 200 : 402
/account  ──POST /billing──▶ Customer portal (switch plan, cancel, update card)
```

## Run it

```sh
# terminal 1
stripe listen --forward-to localhost:4242/webhook

# terminal 2
export STRIPE_SECRET_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)
go run ./guides/05-subscriptions
```

Open <http://localhost:4242>, pick a plan, pay with `4242 4242 4242 4242`.
`/account` shows **active**, `/pro-feature` unlocks. Click **Manage billing**
to switch to yearly or cancel — the change shows up on `/account`.

## The code, step by step

**1. Create the plans once** (lookup keys make it safe to re-run)

```go
monthly, _ := client.EnsurePlan(ctx, stripe.PlanParams{
    LookupKey: "guide_pro_monthly", ProductName: "Pro",
    Amount: stripe.Dollars(20), Interval: stripe.Monthly,
})
yearly, _ := client.EnsurePlan(ctx, stripe.PlanParams{
    LookupKey: "guide_pro_yearly", ProductID: monthly.Product.ID, // same product
    Amount: stripe.Dollars(200), Interval: stripe.Yearly,
})
```

**2. Send the user to Checkout**

```go
session, err := client.CheckoutSubscription(ctx, stripe.SubscriptionCheckoutParams{
    PriceID:           price,
    Customer:          user.CustomerID, // create with client.CreateCustomer on first use
    ClientReferenceID: user.ID,
    Metadata:          map[string]string{"user_id": user.ID}, // copied onto the subscription
    SuccessURL:        "http://localhost:4242/account?session_id={CHECKOUT_SESSION_ID}",
    CancelURL:         "http://localhost:4242/",
})
http.Redirect(w, r, session.URL, http.StatusSeeOther)
```

**3. Keep your copy in sync with webhooks**

```go
hooks := client.Webhooks()
save := func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
    db.SaveSubscription(sub.Metadata["user_id"], sub) // status, period end, cancel_at_period_end
    return nil
}
stripe.On(hooks, stripe.EventCustomerSubscriptionCreated, save)
stripe.On(hooks, stripe.EventCustomerSubscriptionUpdated, save)
stripe.On(hooks, stripe.EventCustomerSubscriptionDeleted, save)
mux.Handle("POST /webhook", hooks)
```

**4. Gate features**

```go
if !stripe.HasAccess(user.Sub) { // active or trialing
    http.Error(w, "Pro subscription required", http.StatusPaymentRequired)
    return
}
until := stripe.SubscriptionAccessUntil(user.Sub) // trial end, cancel date or period end
```

**5. Let customers manage billing themselves**

```go
// once at startup (or save one in Dashboard → Settings → Billing → Customer portal)
portal, _ := client.CreatePortalConfiguration(ctx, stripe.PortalConfigParams{
    AllowCancel: true, AllowUpdatePaymentMethod: true, AllowInvoiceHistory: true,
    SwitchPrices: []string{monthly.ID, yearly.ID},
})

ps, _ := client.CustomerPortal(ctx, stripe.PortalParams{
    Customer: user.CustomerID, ReturnURL: "http://localhost:4242/account",
    ConfigurationID: portal.ID,
})
http.Redirect(w, r, ps.URL, http.StatusSeeOther)
```

## Test it

| Try                                  | How                                              | Result                                   |
| ------------------------------------ | ------------------------------------------------ | ---------------------------------------- |
| Subscribe                            | Checkout with `4242 4242 4242 4242`              | `/account` → `active`, `/pro-feature` 200 |
| 3-D Secure                           | `4000 0025 0000 3155`                            | auth popup, then `active`                |
| Declined                             | `4000 0000 0000 0002`                            | error on Checkout, no subscription       |
| Switch to yearly                     | Manage billing → Update plan                     | `customer.subscription.updated`          |
| Cancel                               | Manage billing → Cancel                          | still `active`, cancels at period end: `true` |
| Subscription events without browser | `stripe trigger customer.subscription.updated`   | logged as unknown customer (expected)    |

```sh
# cancel from the terminal and watch /account update
stripe subscriptions cancel sub_...
```

> Each start creates a new portal configuration in your test account. In
> production, create one once (or use the Dashboard default) and store its id.

## Next

→ [06 · Free trials](../06-free-trials)
