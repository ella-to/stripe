package stripe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	sgo "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"
)

// maxWebhookBody caps how much of the request body we read. Stripe payloads are
// small; this guards against abusive callers.
const maxWebhookBody = 1 << 20 // 1 MiB

// --- Webhook endpoint management ----------------------------------------

// WebhookEndpointParams describes a webhook endpoint to create or update.
type WebhookEndpointParams struct {
	URL string
	// Events is the list of event types to subscribe to, e.g.
	// []string{"invoice.paid", "customer.subscription.deleted"}. Use ["*"] for
	// all events.
	Events      []string
	Description string
	// Connect, when true, also delivers events from connected accounts.
	Connect  bool
	Metadata map[string]string
}

// CreateWebhookEndpoint registers a new webhook endpoint with Stripe. The
// returned WebhookEndpoint.Secret is the signing secret for that endpoint -
// store it and use it to verify deliveries (see Dispatcher).
func (c *Client) CreateWebhookEndpoint(ctx context.Context, p WebhookEndpointParams) (*WebhookEndpoint, error) {
	if p.URL == "" || len(p.Events) == 0 {
		return nil, fmt.Errorf("stripe: CreateWebhookEndpoint requires URL and at least one event")
	}
	params := &sgo.WebhookEndpointCreateParams{
		URL:           String(p.URL),
		EnabledEvents: stringSlice(p.Events),
	}
	if p.Description != "" {
		params.Description = String(p.Description)
	}
	if p.Connect {
		params.Connect = Bool(true)
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	c.prep(&params.Params)
	return c.api.V1WebhookEndpoints.Create(ctx, params)
}

// UpdateWebhookEndpoint changes the URL and/or subscribed events of an existing
// endpoint. Empty fields are left unchanged.
func (c *Client) UpdateWebhookEndpoint(ctx context.Context, id string, p WebhookEndpointParams) (*WebhookEndpoint, error) {
	params := &sgo.WebhookEndpointUpdateParams{}
	if p.URL != "" {
		params.URL = String(p.URL)
	}
	if len(p.Events) > 0 {
		params.EnabledEvents = stringSlice(p.Events)
	}
	if p.Description != "" {
		params.Description = String(p.Description)
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	c.prep(&params.Params)
	return c.api.V1WebhookEndpoints.Update(ctx, id, params)
}

// DeleteWebhookEndpoint removes a webhook endpoint.
func (c *Client) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	params := &sgo.WebhookEndpointDeleteParams{}
	c.prep(&params.Params)
	_, err := c.api.V1WebhookEndpoints.Delete(ctx, id, params)
	return err
}

// --- Typed webhook dispatch ---------------------------------------------

// rawHandler is the type-erased form a registered handler is stored as.
type rawHandler func(ctx context.Context, ev Event) error

// Dispatcher verifies incoming Stripe webhook deliveries, decodes them into the
// right Go type and fans them out to registered handlers.
//
// Handlers run in their own goroutines so the HTTP response (a 200) is returned
// to Stripe immediately - Stripe requires a fast acknowledgement and retries on
// timeout. Register handlers with the package level On function, which uses
// generics to give each handler a strongly typed object.
type Dispatcher struct {
	secret string

	mu       sync.RWMutex
	handlers map[EventType][]rawHandler

	onError    func(ev Event, err error)
	forwardURL string
	httpClient *http.Client
	wg         sync.WaitGroup
}

// DispatcherOption customises a Dispatcher.
type DispatcherOption func(*Dispatcher)

// WithErrorHandler registers a callback invoked whenever a handler returns an
// error or a payload fails to decode. Without it, such errors are silently
// dropped (the event has already been acknowledged to Stripe).
func WithErrorHandler(fn func(ev Event, err error)) DispatcherOption {
	return func(d *Dispatcher) { d.onError = fn }
}

// WithForwardURL configures the Dispatcher to POST the raw JSON payload of
// every verified event to targetURL after running local handlers. Use this to
// relay events to the SASS platform's own webhook endpoint so it receives both
// the verified raw event and any transformations your handlers apply.
//
// The forward is best-effort and fires in the same goroutine pool as handlers;
// forwarding errors are delivered to the WithErrorHandler callback.
func WithForwardURL(targetURL string) DispatcherOption {
	return func(d *Dispatcher) {
		d.forwardURL = targetURL
		if d.httpClient == nil {
			d.httpClient = &http.Client{}
		}
	}
}

// NewDispatcher creates a Dispatcher that verifies payloads with the given
// signing secret ("whsec_...").
func NewDispatcher(secret string, opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{
		secret:   secret,
		handlers: make(map[EventType][]rawHandler),
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Webhooks returns a Dispatcher pre-configured with the secret supplied via
// WithWebhookSecret.
func (c *Client) Webhooks(opts ...DispatcherOption) *Dispatcher {
	return NewDispatcher(c.webhookSecret, opts...)
}

// On registers a strongly typed handler for an event type. T is the Go type the
// event's data object is decoded into, e.g.
//
//	stripe.On(d, stripe.EventInvoicePaid, func(ctx context.Context, ev stripe.Event, inv *stripe.Invoice) error {
//	    log.Printf("invoice %s paid", inv.ID)
//	    return nil
//	})
//
// The raw event JSON is validated by unmarshalling it into *T; if it does not
// fit, the configured error handler is invoked and the typed handler is skipped.
//
// On is a package level function (not a method) because Go methods cannot have
// their own type parameters.
func On[T any](d *Dispatcher, eventType EventType, handler func(ctx context.Context, ev Event, obj *T) error) {
	wrapped := func(ctx context.Context, ev Event) error {
		var obj T
		if len(ev.Data.Raw) > 0 {
			if err := json.Unmarshal(ev.Data.Raw, &obj); err != nil {
				return fmt.Errorf("stripe: decode %s into %T: %w", eventType, obj, err)
			}
		}
		return handler(ctx, ev, &obj)
	}
	d.mu.Lock()
	d.handlers[eventType] = append(d.handlers[eventType], wrapped)
	d.mu.Unlock()
}

// ServeHTTP implements http.Handler. It verifies the Stripe signature, decodes
// the event, schedules matching handlers on goroutines and responds 200
// immediately. Mount it on your webhook route:
//
//	http.Handle("/stripe/webhook", dispatcher)
func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}

	event, err := webhook.ConstructEvent(payload, r.Header.Get("Stripe-Signature"), d.secret)
	if err != nil {
		// A bad signature is the one case we must reject so Stripe knows the
		// delivery was not accepted.
		http.Error(w, "signature verification failed", http.StatusBadRequest)
		return
	}

	d.DispatchRaw(r.Context(), event, payload)
	w.WriteHeader(http.StatusOK)
}

// Dispatch runs the handlers registered for event in their own goroutines. It
// is exported so events obtained elsewhere (e.g. from a queue) can be fanned
// out through the same typed handlers. It returns immediately.
//
// The provided context is detached for the handler goroutines (a background
// context is used) so handlers are not cancelled when the HTTP request returns.
func (d *Dispatcher) Dispatch(ctx context.Context, event Event) {
	d.DispatchRaw(ctx, event, nil)
}

// DispatchRaw is like Dispatch but also accepts the raw JSON payload so that
// the WithForwardURL forwarder can relay the original bytes to the SASS
// platform endpoint unchanged. Pass nil payload when the raw bytes are not
// available (e.g. when the event came from a queue).
func (d *Dispatcher) DispatchRaw(_ context.Context, event Event, payload []byte) {
	d.mu.RLock()
	handlers := d.handlers[EventType(event.Type)]
	forwardURL := d.forwardURL
	d.mu.RUnlock()

	for _, h := range handlers {
		h := h
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			defer func() {
				if rec := recover(); rec != nil && d.onError != nil {
					d.onError(event, fmt.Errorf("stripe: handler panicked: %v", rec))
				}
			}()
			if err := h(context.Background(), event); err != nil && d.onError != nil {
				d.onError(event, err)
			}
		}()
	}

	// Forward the raw payload to the SASS platform endpoint if configured.
	if forwardURL != "" && len(payload) > 0 {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			resp, err := d.httpClient.Post(forwardURL, "application/json", bytes.NewReader(payload))
			if err != nil {
				if d.onError != nil {
					d.onError(event, fmt.Errorf("stripe: forward to %s: %w", forwardURL, err))
				}
				return
			}
			resp.Body.Close()
		}()
	}
}

// Wait blocks until all in-flight handler goroutines have finished. Call it
// during graceful shutdown so events are not lost mid-processing.
func (d *Dispatcher) Wait() { d.wg.Wait() }
