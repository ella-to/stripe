# 00 · Setup

Get a test key, install the Stripe CLI, and check that everything works.

## 1. Get your test API key

Dashboard → **Developers → API keys** (make sure the **Test mode** toggle is on).
Copy the **Secret key** (`sk_test_...`).

```sh
export STRIPE_SECRET_KEY=sk_test_...
```

## 2. Install the Stripe CLI

```sh
# macOS
brew install stripe/stripe-cli/stripe

# other platforms: https://docs.stripe.com/stripe-cli
stripe --version
```

## 3. Log in

```sh
stripe login        # opens the browser once, pairs the CLI with your account
```

## 4. Run the check

```sh
go run ./guides/00-setup
```

```text
INFO created customer id=cus_...
INFO deleted customer id=cus_...
INFO ✅ your Stripe test key works
```

## 5. Try the webhook loop

Every web guide uses the same three-terminal loop:

```sh
# terminal 1: forward Stripe events to your local server
stripe listen --forward-to localhost:4242/webhook

# terminal 2: run a guide (it listens on :4242)
export STRIPE_WEBHOOK_SECRET=$(stripe listen --print-secret)
go run ./guides/03-one-time-payment

# terminal 3: fire test events, watch API calls
stripe trigger checkout.session.completed
stripe logs tail
```

## Next

→ [01 · Customers](../01-customers)
