# 04 · Shopping cart

A product list with quantities → one Checkout for the whole cart, with a
shipping address, two shipping options and promotion codes.

```text
/ (pick quantities) ──POST /checkout──▶ Stripe Checkout (address, shipping, promo code)
                                              │
      /success?session_id=... ◀───────────────┤
                                              └── checkout.session.completed ──▶ /webhook ──▶ ship it
```

## Run it

```sh
# terminal 1
stripe listen --forward-to localhost:4242/webhook

# terminal 2
export STRIPE_SECRET_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)
go run ./guides/04-shopping-cart
```

Open <http://localhost:4242>, choose some quantities, **Checkout**, enter any US
or Canadian address, pick a shipping option and pay with `4242 4242 4242 4242`.

## The code, step by step

**1. Build the cart from the form**

```go
cart := stripe.NewCart("usd")
for _, p := range catalog {
    if qty, _ := strconv.ParseInt(r.FormValue(p.ID), 10, 64); qty > 0 {
        cart.AddItem(p.Name, p.Price, qty)
    }
}
```

**2. Add shipping**

```go
cart.ShipTo("US", "CA").                                   // collect an address
    AddShipping("Standard (5-7 days)", stripe.Dollars(5)). // buyer picks one
    AddShipping("Express (1-2 days)", stripe.Dollars(15))
```

**3. Open Checkout with promo codes**

```go
session, err := client.Checkout(ctx, stripe.CheckoutParams{
    Cart:                cart,
    ClientReferenceID:   order.ID,
    AllowPromotionCodes: true,
    SuccessURL:          "http://localhost:4242/success?session_id={CHECKOUT_SESSION_ID}",
    CancelURL:           "http://localhost:4242/",
})
http.Redirect(w, r, session.URL, http.StatusSeeOther)
```

**4. Fulfil in the webhook**

```go
stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
    addr := cs.CollectedInformation.ShippingDetails // name + address to ship to
    total := cs.AmountTotal                         // items + shipping − discount
    // mark order cs.ClientReferenceID as paid, ship to addr
    return nil
})
```

**5. Show the receipt**

```go
cs, _ := client.GetCheckoutSession(ctx, sessionID)
cs.LineItems.Data                 // what was bought
cs.TotalDetails.AmountShipping    // shipping paid
cs.TotalDetails.AmountDiscount    // promo code discount
```

## Test it

**Create a promotion code:** Dashboard → **Product catalog → Coupons** → create a
coupon (e.g. 20% off) → **Add promotion code** → code `SAVE20`. Then type
`SAVE20` on the Checkout page.

| Try                    | How                                   | Result                         |
| ---------------------- | ------------------------------------- | ------------------------------ |
| Normal order           | `4242 4242 4242 4242`                 | order → `paid` with address    |
| Express shipping       | pick "Express" on Checkout            | +$15.00 on the total           |
| Promo code             | enter `SAVE20`                        | discount on `/success`         |
| Declined card          | `4000 0000 0000 0002`                 | error on Checkout, `pending`   |
| Unsupported country    | pick a country other than US/CA       | not selectable                 |

```sh
stripe trigger checkout.session.completed   # unknown order → logged as a warning
stripe checkout sessions expire cs_test_... # order → expired
```

## Next

→ [05 · Subscriptions](../05-subscriptions)
