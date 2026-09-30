# 03 · One-time payment

A product page with a **Buy** button → Stripe Checkout → success page → order
marked paid by a webhook.

```text
/  ──POST /buy──▶ Stripe Checkout ──▶ /success?session_id=...
                        │
                        └── checkout.session.completed ──▶ /webhook ──▶ order = paid
```

## Run it

```sh
# terminal 1
stripe listen --forward-to localhost:4242/webhook

# terminal 2
export STRIPE_SECRET_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)
go run ./guides/03-one-time-payment
```

Open <http://localhost:4242>, click **Buy now**, pay with `4242 4242 4242 4242`
(any future date, any CVC). The order shows as **paid** on `/orders`.

## The code, step by step

**1. Create an order, then a Checkout session for it**

```go
cart := stripe.NewCart("usd").AddItem("Stripe T-Shirt", stripe.Dollars(25), 1)
session, err := client.Checkout(ctx, stripe.CheckoutParams{
    Cart:              cart,
    ClientReferenceID: order.ID,
    SuccessURL:        "http://localhost:4242/success?session_id={CHECKOUT_SESSION_ID}",
    CancelURL:         "http://localhost:4242/",
})
http.Redirect(w, r, session.URL, http.StatusSeeOther)
```

**2. Fulfil in the webhook** (the buyer may close the tab before `/success` loads)

```go
hooks := client.Webhooks()
stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
    if cs.PaymentStatus == "paid" {
        markPaid(cs.ClientReferenceID, cs.PaymentIntent.ID) // keep the PaymentIntent for refunds
    }
    return nil
})
mux.Handle("POST /webhook", hooks)
```

**3. Show the result on the success page**

```go
cs, err := client.GetCheckoutSession(ctx, r.URL.Query().Get("session_id"))
// cs.PaymentStatus, cs.LineItems.Data, cs.ClientReferenceID
```

## Test it

| Try                           | Card                  | Result                        |
| ----------------------------- | --------------------- | ----------------------------- |
| Successful payment            | `4242 4242 4242 4242` | order → `paid`                |
| 3-D Secure                    | `4000 0025 0000 3155` | auth popup, then `paid`       |
| Declined                      | `4000 0000 0000 0002` | error on Checkout, stays `pending` |
| Abandoned (expire it now)     | —                     | see below → `expired`         |

```sh
# fire a completed event without the browser (unknown order → logged as a warning)
stripe trigger checkout.session.completed

# expire an open session immediately
stripe checkout sessions expire cs_test_...
```

## Next

→ [04 · Shopping cart](../04-shopping-cart)
