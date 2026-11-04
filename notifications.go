package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sync"

	sgo "github.com/stripe/stripe-go/v87"
)

// --- Thin event notifications ---------------------------------------------
//
// Stripe can deliver events in two formats. A classic webhook endpoint sends a
// "snapshot" of the object, serialized in the endpoint's pinned API version
// (handled by Dispatcher). An event destination with the "thin" payload sends
// only the event type and a reference to the object; the handler then fetches
// the object's current state with this SDK's API version.
//
// Because a thin payload carries no versioned object, it never fails an API
// version check: upgrading stripe-go does not require recreating the event
// destination. Fetched objects are also always fresh, so out-of-order or
// retried deliveries cannot apply stale data. The cost is one API call per
// handled notification.

// Notification is the thin payload of an event notification: its id, type,
// creation time and, for events from connected accounts, Context (the account
// the event belongs to).
type Notification = sgo.V2CoreEventNotification

// NotificationType names a thin event, e.g. "v1.invoice.paid". It is the
// classic event type prefixed with "v1.".
type NotificationType string

// A small selection of thin event types, mirroring the Event* constants. Any
// other type supported by Stripe can be used as NotificationType("v1....").
const (
	NotificationCheckoutSessionCompleted         NotificationType = "v1.checkout.session.completed"
	NotificationCustomerSubscriptionCreated      NotificationType = "v1.customer.subscription.created"
	NotificationCustomerSubscriptionUpdated      NotificationType = "v1.customer.subscription.updated"
	NotificationCustomerSubscriptionDeleted      NotificationType = "v1.customer.subscription.deleted"
	NotificationCustomerSubscriptionPaused       NotificationType = "v1.customer.subscription.paused"
	NotificationCustomerSubscriptionResumed      NotificationType = "v1.customer.subscription.resumed"
	NotificationCustomerSubscriptionTrialWillEnd NotificationType = "v1.customer.subscription.trial_will_end"
	NotificationInvoicePaid                      NotificationType = "v1.invoice.paid"
	NotificationInvoicePaymentFailed             NotificationType = "v1.invoice.payment_failed"
	NotificationPaymentIntentSucceeded           NotificationType = "v1.payment_intent.succeeded"
	NotificationChargeRefunded                   NotificationType = "v1.charge.refunded"
	NotificationAccountUpdated                   NotificationType = "v1.account.updated"
)

// notificationHandler is the type-erased form a registered handler is stored as.
type notificationHandler func(ctx context.Context, n sgo.EventNotificationContainer) error

// NotificationDispatcher is the thin event counterpart of Dispatcher: it
// verifies deliveries from an event destination, fetches each notification's
// related object and fans it out to handlers registered with OnNotification.
//
// It accepts the same DispatcherOption values as Dispatcher (error handler,
// logger, WithSyncHandlers, WithForwardURL). WithIgnoreAPIVersionMismatch has
// no effect because thin payloads are not versioned. The error handler gets an
// Event with only ID and Type set.
type NotificationDispatcher struct {
	base *Dispatcher // shared configuration, error reporting and wait group
	api  *sgo.Client

	mu       sync.RWMutex
	handlers map[NotificationType][]notificationHandler
}

// Notifications returns a NotificationDispatcher for an event destination
// using the thin payload format. secret is that destination's signing secret
// ("whsec_..."), which differs from your classic webhook endpoint's secret.
// Related objects are fetched with this client's API key.
//
//	nd := client.Notifications(os.Getenv("STRIPE_EVENT_DESTINATION_SECRET"))
//	stripe.OnNotification(nd, stripe.NotificationInvoicePaid,
//	    func(ctx context.Context, n *stripe.Notification, inv *stripe.Invoice) error {
//	        return markPaid(inv.ID)
//	    })
//	http.Handle("POST /stripe/events", nd)
func (c *Client) Notifications(secret string, opts ...DispatcherOption) *NotificationDispatcher {
	if c.logger != nil {
		opts = append([]DispatcherOption{WithDispatcherLogger(c.logger)}, opts...)
	}
	return &NotificationDispatcher{
		base:     NewDispatcher(secret, opts...),
		api:      c.api,
		handlers: make(map[NotificationType][]notificationHandler),
	}
}

// OnNotification registers a handler for a thin event type. Before the
// handler runs, the notification's related object is fetched from the API and
// decoded into *T, e.g. *Invoice for NotificationInvoicePaid. obj is nil for
// event types without a related object. If T does not match the event's
// related object, the error handler is invoked and the handler is skipped.
func OnNotification[T any](nd *NotificationDispatcher, eventType NotificationType, handler func(ctx context.Context, n *Notification, obj *T) error) {
	wrapped := func(ctx context.Context, c sgo.EventNotificationContainer) error {
		obj, err := fetchRelatedObject[T](ctx, c)
		if err != nil {
			return fmt.Errorf("stripe: fetch related object for %s: %w", eventType, err)
		}
		return handler(ctx, c.GetEventNotification(), obj)
	}
	nd.mu.Lock()
	nd.handlers[eventType] = append(nd.handlers[eventType], wrapped)
	nd.mu.Unlock()
}

// fetchRelatedObject loads the notification's related object as *T. Known
// event types expose a typed FetchRelatedObject; unknown ones return the raw
// JSON, which is decoded into T.
func fetchRelatedObject[T any](ctx context.Context, c sgo.EventNotificationContainer) (*T, error) {
	switch n := c.(type) {
	case interface {
		FetchRelatedObject(context.Context) (*T, error)
	}:
		return n.FetchRelatedObject(ctx)
	case *sgo.UnknownEventNotification:
		res, err := n.FetchRelatedObject(ctx)
		if err != nil || res == nil || res.LastResponse == nil {
			return nil, err
		}
		var obj T
		if err := json.Unmarshal(res.LastResponse.RawJSON, &obj); err != nil {
			return nil, err
		}
		return &obj, nil
	}
	if m := reflect.ValueOf(c).MethodByName("FetchRelatedObject"); m.IsValid() {
		return nil, fmt.Errorf("related object is %s, not %T", m.Type().Out(0), (*T)(nil))
	}
	return nil, nil // the event type has no related object
}

// ServeHTTP implements http.Handler. It verifies the signature, then runs the
// matching handlers: in goroutines after answering 200 by default, or inline
// with WithSyncHandlers, answering 500 when a handler fails so Stripe retries.
func (nd *NotificationDispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d := nd.base
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if d.secret == "" {
		err := fmt.Errorf("stripe: event destination secret is not configured (pass it to Client.Notifications)")
		d.onError(Event{}, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}
	notif, err := nd.api.ParseEventNotification(payload, r.Header.Get("Stripe-Signature"), d.secret)
	if err != nil {
		err = fmt.Errorf("stripe: rejected event notification: %w", err)
		d.onError(Event{}, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	n := notif.GetEventNotification()
	ev := Event{ID: n.ID, Type: EventType(n.Type)}
	nd.mu.RLock()
	handlers := nd.handlers[NotificationType(n.Type)]
	nd.mu.RUnlock()

	if d.sync {
		var firstErr error
		for _, h := range handlers {
			if err := d.runHandler(r.Context(), adaptNotificationHandler(h, notif), ev); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		d.forward(ev, payload)
		if firstErr != nil {
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	for _, h := range handlers {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			_ = d.runHandler(context.Background(), adaptNotificationHandler(h, notif), ev)
		}()
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.forward(ev, payload)
	}()
	w.WriteHeader(http.StatusOK)
}

// adaptNotificationHandler binds a notification to h so it can run through
// Dispatcher.runHandler (panic recovery and error reporting).
func adaptNotificationHandler(h notificationHandler, n sgo.EventNotificationContainer) rawHandler {
	return func(ctx context.Context, _ Event) error { return h(ctx, n) }
}

// Wait blocks until all in-flight handler goroutines have finished.
func (nd *NotificationDispatcher) Wait() { nd.base.Wait() }
