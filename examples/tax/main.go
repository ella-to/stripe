// Command tax demonstrates creating tax rates per jurisdiction, both on the
// platform account and on a connected account.
package main

import (
	"context"
	"log/slog"
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
		fatal("create CA tax", "err", err)
	}
	slog.Info("platform tax rate", "id", caTax.ID)

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
			fatal("create connected-account VAT", "err", err)
		}
		slog.Info("connected-account VAT rate", "id", vat.ID)
	}

	// List active tax rates.
	rates, err := client.ListTaxRates(ctx, true)
	if err != nil {
		fatal("list tax rates", "err", err)
	}
	slog.Info("active tax rates", "count", len(rates))

	// Archive a tax rate (Stripe's form of deletion).
	if _, err := client.DeactivateTaxRate(ctx, caTax.ID); err != nil {
		fatal("deactivate tax", "err", err)
	}
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
