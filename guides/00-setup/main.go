// Guide 00: check that your Stripe test key works. See README.md.
package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"ella.to/stripe"
)

func main() {
	key := os.Getenv("STRIPE_SECRET_KEY")
	if key == "" {
		fatal("STRIPE_SECRET_KEY is not set (Dashboard > Developers > API keys)")
	}
	if !strings.HasPrefix(key, "sk_test_") && !strings.HasPrefix(key, "rk_test_") {
		fatal("use a TEST mode key (sk_test_...) for the guides")
	}

	client := stripe.New(key)
	ctx := context.Background()

	cus, err := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
		Email: "setup-check@example.com",
		Name:  "Setup Check",
	})
	if err != nil {
		fatal("Stripe rejected the request", "err", err)
	}
	slog.Info("created customer", "id", cus.ID)

	if err := client.DeleteCustomer(ctx, cus.ID); err != nil {
		fatal("delete customer", "err", err)
	}
	slog.Info("deleted customer", "id", cus.ID)
	slog.Info("✅ your Stripe test key works")
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
