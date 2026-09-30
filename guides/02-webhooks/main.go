// Guide 02: a verified, typed webhook endpoint. See README.md.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"ella.to/stripe"
)

const addr = "localhost:4242"

// seen remembers processed event ids. Stripe may deliver an event more than
// once, so handlers must be idempotent. Use a database table in production.
var (
	mu   sync.Mutex
	seen = map[string]bool{}
)

// firstTime reports whether ev has not been processed before.
func firstTime(ev stripe.Event) bool {
	mu.Lock()
	defer mu.Unlock()
	if seen[ev.ID] {
		slog.Info("duplicate event skipped", "id", ev.ID, "type", ev.Type)
		return false
	}
	seen[ev.ID] = true
	return true
}

func main() {
	client := stripe.New(
		mustEnv("STRIPE_SECRET_KEY"),
		stripe.WithWebhookSecret(mustEnv("STRIPE_WEBHOOK_SECRET")),
		stripe.WithLogger(slog.Default()), // where rejected deliveries / handler errors go (the default)
	)

	hooks := client.Webhooks(
		stripe.WithIgnoreAPIVersionMismatch(), // local testing: accept your account's API version
		// stripe.WithSyncHandlers(),          // run handlers before replying; errors -> 500 -> Stripe retries
		// stripe.WithErrorHandler(func(ev stripe.Event, err error) { ... }), // custom alerting
	)

	stripe.On(hooks, stripe.EventCheckoutSessionCompleted, func(ctx context.Context, ev stripe.Event, cs *stripe.CheckoutSession) error {
		if !firstTime(ev) {
			return nil
		}
		slog.Info("checkout completed", "session", cs.ID, "mode", cs.Mode, "payment_status", cs.PaymentStatus, "ref", cs.ClientReferenceID)
		return nil
	})

	subChanged := func(ctx context.Context, ev stripe.Event, sub *stripe.Subscription) error {
		if !firstTime(ev) {
			return nil
		}
		customer := ""
		if sub.Customer != nil {
			customer = sub.Customer.ID
		}
		slog.Info("subscription "+string(ev.Type), "subscription", sub.ID, "customer", customer,
			"status", sub.Status, "has_access", stripe.HasAccess(sub))
		return nil
	}
	stripe.On(hooks, stripe.EventCustomerSubscriptionCreated, subChanged)
	stripe.On(hooks, stripe.EventCustomerSubscriptionUpdated, subChanged)
	stripe.On(hooks, stripe.EventCustomerSubscriptionDeleted, subChanged)

	stripe.On(hooks, stripe.EventInvoicePaid, func(ctx context.Context, ev stripe.Event, inv *stripe.Invoice) error {
		if !firstTime(ev) {
			return nil
		}
		slog.Info("invoice paid", "invoice", inv.ID, "amount_paid", inv.AmountPaid, "currency", inv.Currency)
		return nil
	})

	stripe.On(hooks, stripe.EventInvoicePaymentFailed, func(ctx context.Context, ev stripe.Event, inv *stripe.Invoice) error {
		if !firstTime(ev) {
			return nil
		}
		slog.Warn("invoice payment failed: email the customer", "invoice", inv.ID, "amount_due", inv.AmountDue)
		return nil
	})

	stripe.On(hooks, stripe.EventPaymentIntentSucceeded, func(ctx context.Context, ev stripe.Event, pi *stripe.PaymentIntent) error {
		if !firstTime(ev) {
			return nil
		}
		slog.Info("payment succeeded", "payment_intent", pi.ID, "amount", pi.Amount, "currency", pi.Currency)
		return nil
	})

	mux := http.NewServeMux()
	mux.Handle("POST /webhook", hooks)
	srv := &http.Server{Addr: addr, Handler: mux}

	// Graceful shutdown: stop accepting requests, then let running handlers finish.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("listening", "webhook", "http://"+addr+"/webhook")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("server stopped", "err", err)
	}
	hooks.Wait()
	slog.Info("all webhook handlers finished, bye")
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
