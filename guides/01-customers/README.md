# 01 · Customers

Every payment, subscription and saved card belongs to a Stripe **customer**.
Create one per user and store its `cus_...` id on your user record.

```text
your user (u-42)  ──StripeCustomerID──▶  Stripe customer (cus_...)
                  ◀──metadata.app_user_id──
```

## Run it

```sh
export STRIPE_SECRET_KEY=sk_test_...
go run ./guides/01-customers
```

```text
INFO signup user=u-42 customer=cus_...
INFO no duplicate same_customer=true
INFO card attached payment_method=pm_... last4=4242
INFO loaded customer=cus_... email=ada@example.com app_user_id=u-42
INFO email updated customer=cus_... email=ada.lovelace@example.com
INFO deleted customer=cus_...
```

## The code, step by step

**1. Signup: get or create**

```go
func getOrCreateCustomer(ctx context.Context, client *stripe.Client, u *user) (*stripe.Customer, error) {
    if u.StripeCustomerID != "" {
        return client.GetCustomer(ctx, u.StripeCustomerID)
    }
    if cus, found, err := client.FindCustomerByEmail(ctx, u.Email); err != nil || found {
        return cus, err
    }
    return client.CreateCustomer(ctx, stripe.CreateCustomerParams{
        Email:    u.Email,
        Name:     u.Name,
        Metadata: map[string]string{"app_user_id": u.ID},
    })
}

cus, _ := getOrCreateCustomer(ctx, client, u)
u.StripeCustomerID = cus.ID // save it in your database
```

**2. Save a card** (so subscriptions and `ChargeCustomer` can bill it)

```go
pm, _ := client.AttachPaymentMethod(ctx, u.StripeCustomerID, "pm_card_visa")

// or in one go at signup:
client.CreateCustomer(ctx, stripe.CreateCustomerParams{Email: u.Email, PaymentMethod: "pm_card_visa"})
```

**3. Keep it in sync**

```go
client.UpdateCustomer(ctx, u.StripeCustomerID, stripe.UpdateCustomerParams{Email: newEmail})
```

**4. Delete with the account**

```go
client.DeleteCustomer(ctx, u.StripeCustomerID) // also cancels their subscriptions
```

## Test it

```sh
stripe customers list --limit 3              # see what the guide created
stripe customers list --email ada@example.com
stripe logs tail                             # watch the API calls live
```

Running the guide again is safe: the customer is deleted at the end.

## Next

→ [02 · Webhooks](../02-webhooks)
