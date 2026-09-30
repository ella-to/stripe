# 10 · Tax

Add tax at checkout in two ways:

- **Tax rates you manage** (e.g. "Sales Tax 8.25%"): works in any account.
- **Stripe Tax**: Stripe works out the right tax from the customer's address.

```text
/ ──POST /buy/manual────▶ Checkout (+8.25%)        ──▶ /success  (total, tax)
  ──POST /buy/automatic─▶ Checkout (tax by address) ──▶ /webhook  logs total + tax
```

## Run it

```sh
# terminal 1
stripe listen --forward-to localhost:4242/webhook

# terminal 2
export STRIPE_SECRET_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)
go run ./guides/10-tax
```

Open <http://localhost:4242>, click **Buy (8.25% sales tax rate)**, pay with
`4242 4242 4242 4242`. Checkout shows $40.00 + $3.30 tax.

**Stripe Tax button:** activate Stripe Tax first (Dashboard → **Tax** → add
your origin address, test mode is fine). Until then, the page shows Stripe's error.

## The code, step by step

**1. Create a tax rate once, reuse it on restart**

```go
rates, _ := client.ListTaxRates(ctx, true) // active only
// ...reuse the one with the same DisplayName + Percentage, else:
salesTax, err := client.CreateTaxRate(ctx, stripe.TaxRateParams{
    DisplayName: "Sales Tax",
    Percentage:  8.25,
    Country:     "US",
    State:       "TX",
    TaxType:     "sales_tax",
    // Inclusive: true  → the price already contains the tax (typical for VAT)
})
```

**2a. Manual tax: apply the rate to the cart**

```go
cart := stripe.NewCart("usd").
    AddItem("Desk Lamp", stripe.Dollars(40), 1).
    WithTaxRates(salesTax.ID) // or per item: stripe.CartItem{..., TaxRateIDs: []string{id}}
```

**2b. Stripe Tax: let Stripe calculate it from the address**

```go
cart := stripe.NewCart("usd").
    AddItem("Desk Lamp", stripe.Dollars(40), 1).
    ShipTo("US", "CA", "GB", "DE").
    WithAutomaticTax()
```

**3. Checkout as usual, read the tax from the session**

```go
session, err := client.Checkout(ctx, stripe.CheckoutParams{Cart: cart, SuccessURL: ..., CancelURL: ...})

stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
    slog.Info("paid", "total", cs.AmountTotal, "tax", cs.TotalDetails.AmountTax)
    return nil
})
```

**Retire a rate** (tax rates can't be edited or deleted):

```go
client.DeactivateTaxRate(ctx, salesTax.ID)
```

**On a connected account** (marketplaces): `client.CreateTaxRateForAccount(ctx, "acct_...", params)`.

## Test it

| Try                                  | Address                         | Result                    |
| ------------------------------------ | ------------------------------- | ------------------------- |
| Manual rate                          | anything                        | +8.25% on every order     |
| Stripe Tax, US                       | e.g. 1 Market St, San Francisco CA 94105 | CA sales tax (if you registered for CA) |
| Stripe Tax, Germany                  | e.g. Unter den Linden 1, 10117 Berlin | 19% VAT (if you registered for DE) |

Stripe Tax only charges tax where you've added a **registration**
(Dashboard → Tax → Registrations); elsewhere tax is $0.

```sh
stripe tax_rates list --active=true --limit 5
stripe trigger checkout.session.completed
```

## Next

→ [11 · Marketplace (Connect)](../11-marketplace)
