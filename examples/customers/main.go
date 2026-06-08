// Command customers demonstrates customer lifecycle management: create, get,
// update and delete. Customers are the foundation for subscriptions, charges
// and quota — you need a customer id before calling any of those.
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

	// Create an end user (the person who rents equipment on the SASS platform).
	cus, err := client.CreateCustomer(ctx, stripe.CreateCustomerParams{
		Email:       "renter@example.com",
		Name:        "Alice Renter",
		Description: "Equipment renter via RentEasy platform",
		Metadata:    map[string]string{"platform_user_id": "user-42"},
	})
	if err != nil {
		log.Fatalf("create customer: %v", err)
	}
	fmt.Println("created customer:", cus.ID)

	// Retrieve the customer later (e.g. from a webhook handler).
	fetched, err := client.GetCustomer(ctx, cus.ID)
	if err != nil {
		log.Fatalf("get customer: %v", err)
	}
	fmt.Println("fetched:", fetched.Email)

	// Update the customer's email when they change it in the SASS platform.
	updated, err := client.UpdateCustomer(ctx, cus.ID, stripe.UpdateCustomerParams{
		Email: "alice.new@example.com",
	})
	if err != nil {
		log.Fatalf("update customer: %v", err)
	}
	fmt.Println("updated email:", updated.Email)

	// When a user closes their account, delete them from Stripe.
	if err := client.DeleteCustomer(ctx, cus.ID); err != nil {
		log.Fatalf("delete customer: %v", err)
	}
	fmt.Println("customer deleted:", cus.ID)
}
