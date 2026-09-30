// Guide 04: a multi-item cart with shipping and promo codes. See README.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"ella.to/stripe"
)

const addr = "localhost:4242"

type product struct {
	ID    string
	Name  string
	Price int64 // cents
}

var catalog = []product{
	{"tshirt", "T-Shirt", stripe.Dollars(25)},
	{"mug", "Coffee Mug", stripe.Dollars(12)},
	{"stickers", "Sticker Pack", stripe.Dollars(4.50)},
}

type order struct {
	ID      string
	Status  string // "pending", "paid", "expired"
	Total   int64
	ShipTo  string
	Payment string
}

var (
	mu     sync.Mutex
	orders = map[string]*order{}
)

func main() {
	client := stripe.New(
		mustEnv("STRIPE_SECRET_KEY"),
		stripe.WithWebhookSecret(mustEnv("STRIPE_WEBHOOK_SECRET")),
	)

	hooks := client.Webhooks(stripe.WithIgnoreAPIVersionMismatch()) // local testing: accept your account's API version
	stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		if cs.PaymentStatus != "paid" {
			return nil
		}
		// Fulfil: ship to the collected address.
		updateOrder(cs.ClientReferenceID, func(o *order) {
			o.Status = "paid"
			o.Total = cs.AmountTotal
			o.ShipTo = shippingAddress(cs)
			if cs.PaymentIntent != nil {
				o.Payment = cs.PaymentIntent.ID
			}
		})
		return nil
	})
	stripe.On(hooks, stripe.EventCheckoutSessionExpired, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		updateOrder(cs.ClientReferenceID, func(o *order) { o.Status = "expired" })
		return nil
	})

	mux := http.NewServeMux()
	mux.Handle("POST /webhook", hooks)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<h1>Shop</h1><form method="POST" action="/checkout"><table>`)
		for _, p := range catalog {
			fmt.Fprintf(w, `<tr><td>%s</td><td>$%.2f</td><td><input type="number" name="%s" value="0" min="0" max="10"></td></tr>`,
				p.Name, float64(p.Price)/100, p.ID)
		}
		fmt.Fprint(w, `</table><button>Checkout</button></form><p><a href="/orders">Orders</a></p>`)
	})

	mux.HandleFunc("POST /checkout", func(w http.ResponseWriter, r *http.Request) {
		cart := stripe.NewCart("usd")
		for _, p := range catalog {
			if qty, _ := strconv.ParseInt(r.FormValue(p.ID), 10, 64); qty > 0 {
				cart.AddItem(p.Name, p.Price, qty)
			}
		}
		if total, _ := cart.Total(); total == 0 {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		cart.ShipTo("US", "CA").
			AddShipping("Standard (5-7 days)", stripe.Dollars(5)).
			AddShipping("Express (1-2 days)", stripe.Dollars(15))

		o := &order{ID: fmt.Sprintf("order-%d", time.Now().UnixNano()), Status: "pending"}
		session, err := client.Checkout(r.Context(), stripe.CheckoutParams{
			Cart:                cart,
			ClientReferenceID:   o.ID,
			AllowPromotionCodes: true,
			SuccessURL:          "http://" + addr + "/success?session_id={CHECKOUT_SESSION_ID}",
			CancelURL:           "http://" + addr + "/",
		})
		if err != nil {
			slog.Error("create checkout", "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		mu.Lock()
		orders[o.ID] = o
		mu.Unlock()
		http.Redirect(w, r, session.URL, http.StatusSeeOther)
	})

	mux.HandleFunc("GET /success", func(w http.ResponseWriter, r *http.Request) {
		cs, err := client.GetCheckoutSession(r.Context(), r.URL.Query().Get("session_id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "<h1>Thanks!</h1><p>Order %s — payment %s</p><ul>", cs.ClientReferenceID, cs.PaymentStatus)
		if cs.LineItems != nil {
			for _, li := range cs.LineItems.Data {
				fmt.Fprintf(w, "<li>%d × %s — $%.2f</li>", li.Quantity, li.Description, float64(li.AmountTotal)/100)
			}
		}
		fmt.Fprint(w, "</ul>")
		if cs.TotalDetails != nil {
			fmt.Fprintf(w, "<p>Shipping $%.2f · Discount −$%.2f</p>",
				float64(cs.TotalDetails.AmountShipping)/100, float64(cs.TotalDetails.AmountDiscount)/100)
		}
		fmt.Fprintf(w, `<p><b>Total $%.2f</b></p><p>Ship to: %s</p><a href="/orders">Orders</a>`,
			float64(cs.AmountTotal)/100, shippingAddress(cs))
	})

	mux.HandleFunc("GET /orders", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprint(w, "<h1>Orders</h1><ul>")
		for _, o := range orders {
			fmt.Fprintf(w, "<li>%s — <b>%s</b> $%.2f %s</li>", o.ID, o.Status, float64(o.Total)/100, o.ShipTo)
		}
		fmt.Fprint(w, `</ul><a href="/">Shop</a>`)
	})

	slog.Info("listening", "url", "http://"+addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fatal("server stopped", "err", err)
	}
}

func shippingAddress(cs *stripe.CheckoutSession) string {
	if cs.CollectedInformation == nil || cs.CollectedInformation.ShippingDetails == nil ||
		cs.CollectedInformation.ShippingDetails.Address == nil {
		return "-"
	}
	d := cs.CollectedInformation.ShippingDetails
	return fmt.Sprintf("%s, %s, %s %s, %s", d.Name, d.Address.Line1, d.Address.City, d.Address.PostalCode, d.Address.Country)
}

func updateOrder(id string, fn func(*order)) {
	mu.Lock()
	defer mu.Unlock()
	o, ok := orders[id]
	if !ok {
		slog.Warn("webhook for unknown order", "order", id)
		return
	}
	fn(o)
	slog.Info("order updated", "order", o.ID, "status", o.Status, "total", o.Total, "ship_to", o.ShipTo)
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
