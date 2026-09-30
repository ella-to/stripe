# 09 · Usage-based billing

Charge for what customers use: **$2 per started block of 1,000 API calls**,
billed monthly, with optional prepaid credit.

```text
charge = ceil(calls / 1000) × $2        e.g. 1,450 calls → 2 blocks → $4.00
```

## Run it

```sh
export STRIPE_SECRET_KEY=sk_test_...
go run ./guides/09-usage-based-billing
```

```text
INFO metered plan ready meter=mtr_... price=price_...
INFO subscribed customer=cus_... subscription=sub_... status=active
INFO reported usage calls=400
INFO reported usage calls=700
INFO reported usage calls=350
INFO next invoice so far amount_due=$4.00
INFO granted credit grant=credgr_...
INFO voided credit grant=credgr_...
INFO subscription cancelled subscription=sub_...
```

Safe to re-run: the meter and price are reused. Usage is aggregated
asynchronously, so `amount_due` can lag behind for a few seconds.

## The code, step by step

**1. Meter + metered price, created once**

```go
plan, err := client.SetupMeteredQuota(ctx, stripe.SetupMeteredQuotaParams{
    ProductName:      "API calls",
    EventName:        "guide_api_call",
    AmountPerPackage: stripe.Dollars(2),
    PackageSize:      1000,
    Interval:         stripe.Monthly,
    LookupKey:        "guide_api_calls_monthly", // reuse on the next run
})
```

**2. Subscribe a customer** (metered: nothing is charged up front)

```go
sub, err := client.Subscribe(ctx, stripe.SubscribeParams{Customer: cus.ID, PriceID: plan.Price.ID})
```

Via Checkout instead: `client.CheckoutSubscription(ctx, stripe.SubscriptionCheckoutParams{PriceID: basePlan.ID, MeteredPriceIDs: []string{plan.Price.ID}, ...})`.

**3. Report usage** (the `Identifier` makes retries safe)

```go
client.ReportUsageEvent(ctx, stripe.UsageEvent{
    Customer:   cus.ID,
    EventName:  "guide_api_call",
    Value:      400,
    Identifier: requestID,
})
// or simply: client.ReportUsage(ctx, cus.ID, "guide_api_call", 400)
```

**4. Show the bill so far**

```go
inv, err := client.UpcomingInvoice(ctx, sub.ID)
// inv.AmountDue, inv.Lines.Data
```

**5. Prepaid credit that expires**

```go
grant, err := client.GrantQuota(ctx, stripe.QuotaGrantParams{
    Customer:  cus.ID,
    Amount:    stripe.Dollars(2), // = 1,000 free calls
    ExpiresIn: 30 * 24 * time.Hour,
    PriceIDs:  []string{plan.Price.ID},
})
client.VoidCreditGrant(ctx, grant.ID) // remove what's left, e.g. on cancel
```

**Renew the credit every month** from the `invoice.paid` webhook:

```go
stripe.On(hooks, stripe.EventInvoicePaid, func(ctx context.Context, ev stripe.Event, inv *stripe.Invoice) error {
    _, err := client.GrantQuota(ctx, stripe.QuotaGrantParams{
        Customer:  inv.Customer.ID,
        Amount:    stripe.Dollars(2),
        ExpiresIn: 31 * 24 * time.Hour,
        PriceIDs:  []string{meteredPriceID},
    })
    return err
})
```

## Test it

```sh
# report usage from the terminal
stripe post /v1/billing/meter_events -d event_name=guide_api_call \
  -d "payload[stripe_customer_id]=cus_..." -d "payload[value]=2500"

# list your meters
stripe get /v1/billing/meters

stripe trigger invoice.paid     # exercise the renewal handler
```

To see a real month-end invoice without waiting a month, use a
[test clock](https://docs.stripe.com/billing/testing/test-clocks) in the
Dashboard (Billing → Test clocks).

## Next

→ [10 · Tax](../10-tax)
