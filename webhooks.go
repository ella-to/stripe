package stripe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
//
// The endpoint is pinned to the API version this SDK understands, so its
// events always pass the Dispatcher's API version check.
func (c *Client) CreateWebhookEndpoint(ctx context.Context, p WebhookEndpointParams) (*WebhookEndpoint, error) {
	if p.URL == "" || len(p.Events) == 0 {
		return nil, fmt.Errorf("stripe: CreateWebhookEndpoint requires URL and at least one event")
	}
	params := &sgo.WebhookEndpointCreateParams{
		URL:           String(p.URL),
		EnabledEvents: stringSlice(p.Events),
		APIVersion:    String(sgo.APIVersion),
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
// By default handlers run in their own goroutines so the HTTP response (a 200)
// is returned to Stripe immediately - Stripe requires a fast acknowledgement and
// retries on timeout. Use WithSyncHandlers when you would rather have Stripe
// retry the delivery if a handler fails. Register handlers with the package
// level On function, which uses generics to give each handler a strongly typed
// object.
type Dispatcher struct {
	secret string

	mu       sync.RWMutex
	handlers map[EventType][]rawHandler

	onError            func(ev Event, err error)
	logger             *slog.Logger
	forwardURL         string
	httpClient         *http.Client
	ignoreVersionCheck bool
	sync               bool
	wg                 sync.WaitGroup
}

// DispatcherOption customises a Dispatcher.
type DispatcherOption func(*Dispatcher)

// WithErrorHandler registers a callback invoked whenever a delivery is rejected
// (bad signature, API version mismatch), a payload fails to decode, or a
// handler returns an error. For rejected deliveries ev is the zero Event.
// Without it, errors are logged with slog (see WithDispatcherLogger).
func WithErrorHandler(fn func(ev Event, err error)) DispatcherOption {
	return func(d *Dispatcher) { d.onError = fn }
}

// WithDispatcherLogger sets the slog.Logger used to report webhook errors
// when no WithErrorHandler is configured. Defaults to the client's logger (see
// WithLogger) for Client.Webhooks, and slog.Default() for NewDispatcher.
func WithDispatcherLogger(l *slog.Logger) DispatcherOption {
	return func(d *Dispatcher) { d.logger = l }
}

// WithIgnoreAPIVersionMismatch accepts events whose API version differs from
// the one this SDK was built for (stripe-go rejects them by default). This is
// handy with `stripe listen`, which delivers events in your account's default
// API version. Fields that changed between versions may decode incorrectly, so
// prefer pinning your webhook endpoint (or `stripe listen --latest`) to the SDK
// version in production.
func WithIgnoreAPIVersionMismatch() DispatcherOption {
	return func(d *Dispatcher) { d.ignoreVersionCheck = true }
}

// WithSyncHandlers runs handlers inline, before responding to Stripe. If any
// handler returns an error (or panics) the delivery is answered with a 500 so
// Stripe retries it later. Use this when losing an event is worse than a slow
// response, e.g. order fulfillment. Keep handlers fast: Stripe times out after
// a few seconds.
func WithSyncHandlers() DispatcherOption {
	return func(d *Dispatcher) { d.sync = true }
}

// WithForwardURL configures the Dispatcher to POST the raw JSON payload of
// every verified event to targetURL after running local handlers. Use this to
// relay events to another service's webhook endpoint after they have been
// verified here.
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
	if d.onError == nil {
		logger := d.logger
		if logger == nil {
			logger = slog.Default()
		}
		d.onError = func(ev Event, err error) {
			if ev.ID != "" {
				logger.Error("stripe webhook failed", "event_id", ev.ID, "event_type", ev.Type, "err", err)
				return
			}
			logger.Error("stripe webhook rejected", "err", err)
		}
	}
	return d
}

// Webhooks returns a Dispatcher pre-configured with the secret supplied via
// WithWebhookSecret.
func (c *Client) Webhooks(opts ...DispatcherOption) *Dispatcher {
	if c.logger != nil {
		opts = append([]DispatcherOption{WithDispatcherLogger(c.logger)}, opts...)
	}
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
	if d.secret == "" {
		err := fmt.Errorf("stripe: webhook secret is not configured (use WithWebhookSecret or NewDispatcher(\"whsec_...\"))")
		d.onError(Event{}, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}

	event, err := webhook.ConstructEventWithOptions(payload, r.Header.Get("Stripe-Signature"), d.secret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: d.ignoreVersionCheck})
	if err != nil {
		// Reject so Stripe knows the delivery was not accepted. The reason is
		// visible in the Stripe dashboard / `stripe listen` output.
		err = fmt.Errorf("stripe: rejected webhook: %w", err)
		d.onError(Event{}, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if d.sync {
		if err := d.dispatchSync(r.Context(), event, payload); err != nil {
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	d.DispatchRaw(r.Context(), event, payload)
	w.WriteHeader(http.StatusOK)
}

// dispatchSync runs the handlers for event inline and returns the first error.
func (d *Dispatcher) dispatchSync(ctx context.Context, event Event, payload []byte) error {
	d.mu.RLock()
	handlers := d.handlers[EventType(event.Type)]
	d.mu.RUnlock()

	var firstErr error
	for _, h := range handlers {
		if err := d.runHandler(ctx, h, event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	d.forward(event, payload)
	return firstErr
}

// runHandler calls h, converting a panic into an error and reporting any error
// to the error handler.
func (d *Dispatcher) runHandler(ctx context.Context, h rawHandler, event Event) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("stripe: handler panicked: %v", rec)
		}
		if err != nil {
			d.onError(event, err)
		}
	}()
	return h(ctx, event)
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
// the WithForwardURL forwarder can relay the original bytes unchanged. Pass nil payload when the raw bytes are not
// available (e.g. when the event came from a queue).
func (d *Dispatcher) DispatchRaw(_ context.Context, event Event, payload []byte) {
	d.mu.RLock()
	handlers := d.handlers[EventType(event.Type)]
	forwardURL := d.forwardURL
	d.mu.RUnlock()

	for _, h := range handlers {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			_ = d.runHandler(context.Background(), h, event)
		}()
	}

	if forwardURL != "" && len(payload) > 0 {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.forward(event, payload)
		}()
	}
}

// forward relays the raw payload to the WithForwardURL target, if configured.
func (d *Dispatcher) forward(event Event, payload []byte) {
	d.mu.RLock()
	forwardURL := d.forwardURL
	d.mu.RUnlock()
	if forwardURL == "" || len(payload) == 0 {
		return
	}
	resp, err := d.httpClient.Post(forwardURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		d.onError(event, fmt.Errorf("stripe: forward to %s: %w", forwardURL, err))
		return
	}
	resp.Body.Close()
}

// Wait blocks until all in-flight handler goroutines have finished. Call it
// during graceful shutdown so events are not lost mid-processing.
func (d *Dispatcher) Wait() { d.wg.Wait() }
