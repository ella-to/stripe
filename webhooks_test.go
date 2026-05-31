package stripe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sgo "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
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
