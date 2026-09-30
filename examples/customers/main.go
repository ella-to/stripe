// Command customers demonstrates customer lifecycle management: create, get,
// update and delete. Customers are the foundation for subscriptions, charges
// and quota — you need a customer id before calling any of those.
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

	// Create a customer when a user signs up. Store cus.ID on your user record.
	cus, err := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
		Email:       "renter@example.com",
		Name:        "Alice Renter",
		Description: "Signed up via the web app",
		Metadata:    map[string]string{"platform_user_id": "user-42"},
	})
	if err != nil {
		fatal("create customer", "err", err)
	}
	slog.Info("created customer", "id", cus.ID)

	// Retrieve the customer later (e.g. from a webhook handler).
	fetched, err := client.GetCustomer(ctx, cus.ID)
	if err != nil {
		fatal("get customer", "err", err)
	}
	slog.Info("fetched customer", "email", fetched.Email)

	// Keep Stripe in sync when the user changes their email in your app.
	updated, err := client.UpdateCustomer(ctx, cus.ID, stripe.UpdateCustomerParams{
		Email: "alice.new@example.com",
	})
	if err != nil {
		fatal("update customer", "err", err)
	}
	slog.Info("updated customer", "email", updated.Email)

	// When a user closes their account, delete them from Stripe.
	if err := client.DeleteCustomer(ctx, cus.ID); err != nil {
		fatal("delete customer", "err", err)
	}
	slog.Info("deleted customer", "id", cus.ID)
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
