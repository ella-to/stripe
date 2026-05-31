// Command platformfee demonstrates charging a platform fee on connected-account
// transactions using Stripe application fees on direct charges.
//
// With a direct charge the connected account is the settlement merchant, so it
// pays Stripe's processing fee, and your platform collects a separate
// application fee. For a $10.00 charge where Stripe's fee is ~$3.00 and your
// platform fee is $3.00, the seller nets ~$4.00.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"ella.to/stripe"
)

func main() {
	// By default, fees are stored on each connected account's Stripe metadata
	// (AccountMetadataFeeStore). To keep fees in your own database instead, plug
	// in a custom resolver - it is consulted whenever a charge does not pass an
	// explicit fee:
	//
	//   client := stripe.New(key, stripe.WithFeeResolver(stripe.FeeResolverFunc(
	//       func(ctx context.Context, accountID string) (stripe.PlatformFee, bool, error) {
	//           row, ok := db.LookupFee(ctx, accountID)
	//           if !ok {
	//               return stripe.PlatformFee{}, false, nil
	//           }
	//           return stripe.PlatformFee{Percent: row.Percent, Fixed: row.Fixed}, true, nil
	//       },
	//   )))
	client := stripe.New(os.Getenv("STRIPE_SECRET_KEY"))
	ctx := context.Background()

	seller := os.Getenv("STRIPE_CONNECTED_ACCOUNT") // acct_...
	if seller == "" {
		log.Fatal("set STRIPE_CONNECTED_ACCOUNT to a connected account id")
	}

	// 1. Assign a platform fee to this connected account. It is stored on the
	//    account itself, so you can change it any time (see step 4) without a
	//    separate database. Here: a flat $3.00 per transaction.
	if _, err := client.SetPlatformFee(ctx, seller, stripe.PlatformFee{
		Fixed: stripe.Dollars(3),
	}); err != nil {
		log.Fatalf("set platform fee: %v", err)
	}

	// 2. Inspect the computed split locally before charging anything.
	fee, _, err := client.GetPlatformFee(ctx, seller)
	if err != nil {
		log.Fatalf("get platform fee: %v", err)
	}
	gross := stripe.Dollars(10)
	platformCut := fee.Compute(gross)
	fmt.Printf("gross=%d platform_fee=%d (Stripe fee is paid by the seller separately)\n", gross, platformCut)

	// 3. Take a $10 direct charge on the seller. The platform fee is resolved
	//    from the account automatically (Fee left nil). Provide a real, attached
	//    PaymentMethod + Confirm:true to actually capture funds.
	pi, err := client.ChargeWithFee(ctx, stripe.ChargeParams{
		ConnectedAccount: seller,
		Amount:           gross,
		Currency:         "usd",
		Description:      "Order #1001",
		// PaymentMethod: "pm_card_visa",
		// Confirm:       true,
	})
	if err != nil {
		log.Fatalf("charge with fee: %v", err)
	}
	fmt.Printf("payment_intent=%s application_fee=%d status=%s\n", pi.ID, pi.ApplicationFeeAmount, pi.Status)

	// 4. Change the fee at any time. Switch this account to 2.9% + $0.30.
	if _, err := client.SetPlatformFee(ctx, seller, stripe.PlatformFee{
		Percent: 2.9,
		Fixed:   stripe.Dollars(0.30),
	}); err != nil {
		log.Fatalf("update platform fee: %v", err)
	}

	// 5. A different connected account can carry a different fee. Override the
	//    stored fee per call with Fee, or store a different value for them.
	if other := os.Getenv("STRIPE_OTHER_ACCOUNT"); other != "" {
		_, err := client.ChargeWithFee(ctx, stripe.ChargeParams{
			ConnectedAccount: other,
			Amount:           stripe.Dollars(50),
			Fee:              &stripe.PlatformFee{Percent: 10}, // 10% just for this charge
		})
		if err != nil {
			log.Fatalf("charge other account: %v", err)
		}
	}

	// 6. Platform fees also work through hosted Checkout (direct charge) and on
	//    subscriptions (recurring percentage fee):
	//
	//    client.Checkout(ctx, stripe.CheckoutParams{
	//        Cart: cart, ConnectedAccount: seller,
	//        SuccessURL: ..., CancelURL: ...,
	//        // Fee resolved from the account, or set Fee/FeeAmount explicitly.
	//    })
	//
	//    client.Subscribe(ctx, stripe.SubscribeParams{
	//        Customer: "cus_123", PriceID: priceID,
	//        ConnectedAccount: seller, FeePercent: 10, // 10% of every invoice
	//    })
}
