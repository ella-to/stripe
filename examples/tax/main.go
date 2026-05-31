// Command tax demonstrates creating tax rates per jurisdiction, both on the
// platform account and on a connected account.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"ella.to/stripe"
)

func main() {
	client := stripe.New(os.Getenv("STRIPE_SECRET_KEY"))
	ctx := context.Background()

	// A US state sales tax rate on the platform account.
	caTax, err := client.CreateTaxRate(ctx, stripe.TaxRateParams{
		DisplayName:  "CA Sales Tax",
		Percentage:   7.25,
		Country:      "US",
		State:        "CA",
		Jurisdiction: "California",
		TaxType:      "sales_tax",
	})
	if err != nil {
		log.Fatalf("create CA tax: %v", err)
	}
	fmt.Println("platform tax rate:", caTax.ID)

	// An EU VAT rate created on a connected account.
	if acct := os.Getenv("STRIPE_CONNECTED_ACCOUNT"); acct != "" {
		vat, err := client.CreateTaxRateForAccount(ctx, acct, stripe.TaxRateParams{
			DisplayName:  "VAT",
			Percentage:   19,
			Country:      "DE",
			Jurisdiction: "Germany",
			TaxType:      "vat",
			Inclusive:    true,
		})
		if err != nil {
			log.Fatalf("create connected-account VAT: %v", err)
		}
		fmt.Println("connected-account VAT rate:", vat.ID)
	}

	// List active tax rates.
	rates, err := client.ListTaxRates(ctx, true)
	if err != nil {
		log.Fatalf("list tax rates: %v", err)
	}
	fmt.Printf("found %d active tax rates\n", len(rates))

	// Archive a tax rate (Stripe's form of deletion).
	if _, err := client.DeactivateTaxRate(ctx, caTax.ID); err != nil {
		log.Fatalf("deactivate tax: %v", err)
	}
}
