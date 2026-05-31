// Command connect demonstrates Stripe Connect: registering a connected account,
// generating onboarding/update links, connecting an existing account via OAuth
// and deleting a connected account.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"ella.to/stripe"
)

func main() {
	client := stripe.New(
		os.Getenv("STRIPE_SECRET_KEY"),
		stripe.WithOAuthClientID(os.Getenv("STRIPE_CONNECT_CLIENT_ID")),
	)
	ctx := context.Background()

	// 1. Register a new connected (Express) account.
	acct, err := client.RegisterConnectedAccount(ctx, stripe.ConnectedAccountParams{
		Type:         stripe.AccountExpress,
		Email:        "seller@example.com",
		Country:      "US",
		BusinessType: "individual",
		Capabilities: []string{"card_payments", "transfers"},
		Metadata:     map[string]string{"internal_id": "seller-42"},
	})
	if err != nil {
		log.Fatalf("register account: %v", err)
	}
	fmt.Println("created connected account:", acct.ID)

	// 2. Send the seller through hosted onboarding.
	link, err := client.AccountOnboardingLink(ctx, acct.ID,
		"https://app.example.com/reauth",
		"https://app.example.com/return",
	)
	if err != nil {
		log.Fatalf("onboarding link: %v", err)
	}
	fmt.Println("onboard here:", link.URL)

	// 3. Later, request additional/updated information.
	updateLink, err := client.RequestAccountUpdate(ctx, acct.ID,
		"https://app.example.com/reauth",
		"https://app.example.com/return",
	)
	if err != nil {
		log.Fatalf("update link: %v", err)
	}
	fmt.Println("update info here:", updateLink.URL)

	// 4. Patch some account fields directly.
	if _, err := client.UpdateConnectedAccount(ctx, acct.ID, stripe.ConnectedAccountUpdate{
		Metadata: map[string]string{"tier": "gold"},
		Defaults: map[string]string{"business_profile[url]": "https://seller42.example.com"},
	}); err != nil {
		log.Fatalf("update account: %v", err)
	}

	// 5. Connect an EXISTING Stripe account via OAuth.
	//    First send the user to the authorize URL...
	authURL, err := client.ConnectAuthorizeURL("csrf-state-token")
	if err != nil {
		log.Fatalf("authorize url: %v", err)
	}
	fmt.Println("connect existing account here:", authURL)
	//    ...then in your OAuth callback exchange the code:
	//
	//    token, err := client.ConnectExistingAccount(ctx, code)
	//    connectedID := token.StripeUserID // acct_... you can now ForAccount()

	// 6. Delete the connected account when done.
	if err := client.DeleteConnectedAccount(ctx, acct.ID); err != nil {
		log.Fatalf("delete account: %v", err)
	}
	fmt.Println("deleted connected account:", acct.ID)
}
