# 11 · Marketplace (Connect Express)

Sellers onboard with Stripe, buyers pay the seller directly, and your platform
keeps a fee on every sale.

```text
/sell ──▶ Express account + fee ──▶ Stripe onboarding ──▶ /seller/{id}  (ready?)
/store/{id} ──POST buy──▶ Checkout on the seller's account (platform fee) ──▶ webhook ──▶ order = paid
/orders ──Refund──▶ refund on the seller's account (+ fee returned)
```

## Run it

**0. Enable Connect** (once): Dashboard → **Connect** → **Get started**, in test mode.

```sh
# terminal 1: also forward events from connected accounts
stripe listen --forward-to localhost:4242/webhook --forward-connect-to localhost:4242/webhook

# terminal 2
export STRIPE_SECRET_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)
go run ./guides/11-marketplace
```

1. Open <http://localhost:4242/sell>, enter an email → Stripe onboarding opens.
2. Fill it in with test data (use the **test data prefill** option if offered;
   SMS code `000000`, SSN `000-00-0000`, routing `110000000`, account `000123456789`).
3. Back on the seller page you should see **✅ Ready to sell**.
4. Open the store, **Buy**, pay with `4242 4242 4242 4242`.
5. `/orders` shows **paid**; press **Refund** to refund it.

## The code, step by step

**1. Create the seller's account and set their fee**

```go
acct, err := client.RegisterConnectedAccount(ctx, stripe.ConnectedAccountParams{
    Type:         stripe.AccountExpress,
    Email:        email,
    Country:      "US",
    Capabilities: []string{"card_payments", "transfers"},
})
client.SetPlatformFee(ctx, acct.ID, stripe.PlatformFee{Percent: 10, Fixed: stripe.Dollars(0.30)})
```

**2. Send them through onboarding**

```go
link, err := client.AccountOnboardingLink(ctx, acct.ID,
    "http://localhost:4242/onboard/"+acct.ID, // refresh URL: link expired → make a new one
    "http://localhost:4242/seller/"+acct.ID,  // return URL
)
http.Redirect(w, r, link.URL, http.StatusSeeOther)
```

**3. Check if they can sell** (returning from onboarding ≠ finished)

```go
acct, err := client.GetConnectedAccount(ctx, id)
if stripe.AccountReady(acct) { /* show the store */ }

stripe.On(hooks, stripe.EventAccountUpdated, func(ctx context.Context, ev stripe.Event, acct *stripe.Account) error {
    setReady(acct.ID, stripe.AccountReady(acct))
    return nil
})
```

**4. Take a payment for the seller**

```go
session, err := client.Checkout(ctx, stripe.CheckoutParams{
    Cart:              stripe.NewCart("usd").AddItem("Handmade mug", 2000, 1),
    ConnectedAccount:  sellerID, // direct charge; fee read from SetPlatformFee
    ClientReferenceID: orderID,
    SuccessURL:        "http://localhost:4242/orders",
    CancelURL:         "http://localhost:4242/store/" + sellerID,
})
```

The fee math:

```go
fee := stripe.PlatformFee{Percent: 10, Fixed: stripe.Dollars(0.30)}
fee.Compute(2000) // 230 → $2.30 to you; the seller gets $20.00 − $2.30 − Stripe's fee
```

**5. Handle the Connect event** (`ev.Account` is the seller)

```go
stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
    markPaid(cs.ClientReferenceID, ev.Account, cs.PaymentIntent.ID)
    return nil
})
```

**6. Seller dashboard and refunds**

```go
link, _ := client.ExpressDashboardLink(ctx, sellerID) // redirect to link.URL

client.RefundPayment(ctx, stripe.RefundParams{
    PaymentIntentID:      paymentID,
    ConnectedAccount:     sellerID, // the charge lives on the seller's account
    RefundApplicationFee: true,     // return your fee too
})
```

## Test it

```sh
# a Connect event, as if it came from a seller
stripe trigger checkout.session.completed --stripe-account acct_...

# see your connected accounts
stripe accounts list --limit 5

# what your platform earned
stripe application_fees list --limit 5
```

| Try                                    | Result                                            |
| -------------------------------------- | ------------------------------------------------- |
| Close onboarding halfway, open `/seller/{id}` | "Finish onboarding" link                   |
| Buy from a seller who isn't ready      | Checkout fails with an error from Stripe          |
| No `--forward-connect-to`              | Orders stay `pending` (the event never arrives)   |

## Next

→ [12 · Connect OAuth (multi-tenant)](../12-connect-oauth)
