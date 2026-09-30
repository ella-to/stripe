package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sgo "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"
)

// TestDispatcherTypedHandler verifies the generic dispatcher: a signed payload
// is verified, decoded into the correct Go type, and delivered to the handler.
func TestDispatcherTypedHandler(t *testing.T) {
	const secret = "whsec_test_secret"

	// Build an invoice.paid event whose data object is an invoice.
	event := map[string]any{
		"id":          "evt_1",
		"object":      "event",
		"api_version": sgo.APIVersion,
		"type":        string(EventInvoicePaid),
		"data": map[string]any{
			"object": map[string]any{
				"id":          "in_123",
				"object":      "invoice",
				"amount_paid": 4200,
			},
		},
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: time.Now(),
	})

	d := NewDispatcher(secret)

	done := make(chan *Invoice, 1)
	On(d, EventInvoicePaid, func(ctx context.Context, ev Event, inv *Invoice) error {
		done <- inv
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(signed.Payload)))
	req.Header.Set("Stripe-Signature", signed.Header)
	rec := httptest.NewRecorder()

	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	d.Wait() // let the handler goroutine finish

	select {
	case inv := <-done:
		if inv.ID != "in_123" {
			t.Fatalf("expected invoice in_123, got %q", inv.ID)
		}
		if inv.AmountPaid != 4200 {
			t.Fatalf("expected amount_paid 4200, got %d", inv.AmountPaid)
		}
	default:
		t.Fatal("handler was not invoked")
	}
}

// TestDispatcherRejectsBadSignature ensures tampered payloads are rejected.
func TestDispatcherRejectsBadSignature(t *testing.T) {
	d := NewDispatcher("whsec_test_secret")
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	req.Header.Set("Stripe-Signature", "t=1,v1=deadbeef")
	rec := httptest.NewRecorder()

	d.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad signature, got %d", rec.Code)
	}
}

func signedRequest(t *testing.T, secret string, event map[string]any) *http.Request {
	t.Helper()
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: payload, Secret: secret, Timestamp: time.Now(),
	})
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(signed.Payload)))
	req.Header.Set("Stripe-Signature", signed.Header)
	return req
}

func testEvent(apiVersion string) map[string]any {
	return map[string]any{
		"id": "evt_1", "object": "event", "api_version": apiVersion,
		"type": string(EventInvoicePaid),
		"data": map[string]any{"object": map[string]any{"id": "in_1", "object": "invoice"}},
	}
}

func TestDispatcherSyncHandlerError(t *testing.T) {
	const secret = "whsec_test"
	d := NewDispatcher(secret, WithSyncHandlers(), WithErrorHandler(func(Event, error) {}))
	On(d, EventInvoicePaid, func(ctx context.Context, ev Event, inv *Invoice) error {
		return errors.New("db down")
	})
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, signedRequest(t, secret, testEvent(sgo.APIVersion)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 so Stripe retries, got %d", rec.Code)
	}
}

func TestDispatcherMissingSecret(t *testing.T) {
	var reported error
	d := NewDispatcher("", WithErrorHandler(func(_ Event, err error) { reported = err }))
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, signedRequest(t, "whsec_x", testEvent(sgo.APIVersion)))
	if rec.Code != http.StatusInternalServerError || reported == nil {
		t.Fatalf("expected 500 and a reported error, got %d / %v", rec.Code, reported)
	}
}

func TestDispatcherAPIVersionMismatch(t *testing.T) {
	const secret = "whsec_test"
	old := testEvent("2020-08-27")

	var reported error
	strict := NewDispatcher(secret, WithErrorHandler(func(_ Event, err error) { reported = err }))
	rec := httptest.NewRecorder()
	strict.ServeHTTP(rec, signedRequest(t, secret, old))
	if rec.Code != http.StatusBadRequest || reported == nil || !strings.Contains(reported.Error(), "API version") {
		t.Fatalf("expected a 400 API version error, got %d / %v", rec.Code, reported)
	}

	got := make(chan string, 1)
	lenient := NewDispatcher(secret, WithIgnoreAPIVersionMismatch(), WithSyncHandlers())
	On(lenient, EventInvoicePaid, func(ctx context.Context, ev Event, inv *Invoice) error {
		got <- inv.ID
		return nil
	})
	rec = httptest.NewRecorder()
	lenient.ServeHTTP(rec, signedRequest(t, secret, old))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
	if id := <-got; id != "in_1" {
		t.Fatalf("got invoice %q", id)
	}
}

func TestHasAccess(t *testing.T) {
	cases := map[SubscriptionStatus]bool{
		SubscriptionActive: true, SubscriptionTrialing: true,
		SubscriptionPastDue: false, SubscriptionCanceled: false, SubscriptionIncomplete: false,
	}
	for status, want := range cases {
		if got := HasAccess(&Subscription{Status: status}); got != want {
			t.Errorf("HasAccess(%s) = %v, want %v", status, got, want)
		}
	}
	if HasAccess(nil) {
		t.Error("HasAccess(nil) = true")
	}
}
