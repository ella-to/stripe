// Command purchase demonstrates building a cart with multiple items, adding tax
// and shipping, and creating a hosted Checkout session.
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

	// Build a cart. Items first; tax and shipping can be added later.
	cart := stripe.NewCart("usd").
		AddItem("T-Shirt", stripe.Dollars(25), 2).
		AddItem("Sticker pack", stripe.Dollars(5), 1)

	// Reference an existing price too, if you have one.
	if priceID := os.Getenv("STRIPE_PRICE_ID"); priceID != "" {
		cart.AddPrice(priceID, 1)
	}

	// Add tax (automatic, location based) and a couple of shipping options.
	cart.WithAutomaticTax().
		AddShipping("Standard (5-7 days)", stripe.Dollars(5)).
		AddShipping("Express (1-2 days)", stripe.Dollars(15))

	session, err := client.Checkout(ctx, stripe.CheckoutParams{
		Cart:          cart,
		SuccessURL:    "https://shop.example.com/success?session_id={CHECKOUT_SESSION_ID}",
		CancelURL:     "https://shop.example.com/cart",
		CustomerEmail: "buyer@example.com",
		Metadata:      map[string]string{"order_ref": "ORDER-1001"},
	})
	if err != nil {
		log.Fatalf("checkout: %v", err)
	}

	fmt.Println("send the buyer to:", session.URL)
}
