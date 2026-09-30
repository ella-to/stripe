// Guide 01: keep your users and Stripe customers in sync. See README.md.
package main

import (
	"context"
	"log/slog"
	"os"

	"ella.to/stripe"
)

// user is your own user record. StripeCustomerID links it to Stripe.
type user struct {
	ID               string
	Email            string
	Name             string
	StripeCustomerID string
}

func main() {
	client := stripe.New(mustEnv("STRIPE_SECRET_KEY"))
	ctx := context.Background()

	u := &user{ID: "u-42", Email: "ada@example.com", Name: "Ada Lovelace"}

	// 1. Signup: get or create the Stripe customer, store its id.
	cus, err := getOrCreateCustomer(ctx, client, u)
	if err != nil {
		fatal("get or create customer", "err", err)
	}
	u.StripeCustomerID = cus.ID
	slog.Info("signup", "user", u.ID, "customer", cus.ID)

	// 2. Calling it again (e.g. a retried signup) returns the same customer.
	again, err := getOrCreateCustomer(ctx, client, u)
	if err != nil {
		fatal("get or create customer again", "err", err)
	}
	slog.Info("no duplicate", "same_customer", again.ID == cus.ID)

	// 3. Save a card so you can bill them later (test token in test mode).
	pm, err := client.AttachPaymentMethod(ctx, u.StripeCustomerID, "pm_card_visa")
	if err != nil {
		fatal("attach card", "err", err)
	}
	slog.Info("card attached", "payment_method", pm.ID, "last4", pm.Card.Last4)

	// 4. Read it back by the id you stored.
	cus, err = client.GetCustomer(ctx, u.StripeCustomerID)
	if err != nil {
		fatal("get customer", "err", err)
	}
	slog.Info("loaded", "customer", cus.ID, "email", cus.Email, "app_user_id", cus.Metadata["app_user_id"])

	// 5. The user changes their email in your app: update Stripe too.
	u.Email = "ada.lovelace@example.com"
	cus, err = client.UpdateCustomer(ctx, u.StripeCustomerID, stripe.UpdateCustomerParams{Email: u.Email})
	if err != nil {
		fatal("update customer", "err", err)
	}
	slog.Info("email updated", "customer", cus.ID, "email", cus.Email)

	// 6. The user deletes their account: delete the customer (cancels their
	// subscriptions, removes saved cards).
	if err := client.DeleteCustomer(ctx, u.StripeCustomerID); err != nil {
		fatal("delete customer", "err", err)
	}
	slog.Info("deleted", "customer", u.StripeCustomerID)
	u.StripeCustomerID = ""
}

// getOrCreateCustomer returns the user's Stripe customer, creating it only
// when needed. The stored id wins; the email lookup catches duplicates when the
// id was never saved (e.g. a crash between create and save).
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

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		fatal("missing environment variable", "name", name)
	}
	return v
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
