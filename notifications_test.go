package stripe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func thinEvent(eventType, context string) map[string]any {
	ev := map[string]any{
		"id": "evt_thin", "object": "v2.core.event", "type": eventType,
		"livemode": false, "created": "2026-11-01T00:00:00.000Z",
		"related_object": map[string]any{"id": "in_1", "type": "invoice", "url": "/v1/invoices/in_1"},
	}
	if context != "" {
		ev["context"] = context
	}
	return ev
}

func TestNotificationFetchesRelatedObject(t *testing.T) {
	const secret = "whsec_thin"
	client, cs := newCaptureClient(t, `{"id":"in_1","object":"invoice","status":"paid"}`)
	nd := client.Notifications(secret, WithSyncHandlers())

	var gotID, gotStatus, gotEvent string
	OnNotification(nd, NotificationInvoicePaid, func(ctx context.Context, n *Notification, inv *Invoice) error {
		gotEvent, gotID, gotStatus = n.ID, inv.ID, string(inv.Status)
		return nil
	})

	rec := httptest.NewRecorder()
	nd.ServeHTTP(rec, signedRequest(t, secret, thinEvent(string(NotificationInvoicePaid), "acct_1")))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if gotEvent != "evt_thin" || gotID != "in_1" || gotStatus != "paid" {
		t.Fatalf("handler got event %q invoice %q status %q", gotEvent, gotID, gotStatus)
	}
	r := cs.last(t)
	if r.Method != http.MethodGet || r.Path != "/v1/invoices/in_1" {
		t.Fatalf("fetched %s %s", r.Method, r.Path)
	}
	if got := r.Header.Get("Stripe-Context"); got != "acct_1" {
		t.Errorf("Stripe-Context = %q, want acct_1", got)
	}
}

func TestNotificationAsyncAndUnhandled(t *testing.T) {
	const secret = "whsec_thin"
	client, _ := newCaptureClient(t, `{"id":"in_1","object":"invoice"}`)
	nd := client.Notifications(secret)
	got := make(chan string, 1)
	OnNotification(nd, NotificationInvoicePaid, func(ctx context.Context, n *Notification, inv *Invoice) error {
		got <- inv.ID
		return nil
	})

	rec := httptest.NewRecorder()
	nd.ServeHTTP(rec, signedRequest(t, secret, thinEvent(string(NotificationInvoicePaid), "")))
	nd.Wait()
	if rec.Code != http.StatusOK || <-got != "in_1" {
		t.Fatalf("got %d", rec.Code)
	}

	// No handler registered for this type: acknowledged, nothing runs.
	rec = httptest.NewRecorder()
	nd.ServeHTTP(rec, signedRequest(t, secret, thinEvent("v1.invoice.created", "")))
	nd.Wait()
	if rec.Code != http.StatusOK {
		t.Fatalf("unhandled type: got %d", rec.Code)
	}
}

func TestNotificationWrongObjectType(t *testing.T) {
	const secret = "whsec_thin"
	client, _ := newCaptureClient(t, `{"id":"in_1","object":"invoice"}`)
	var reported error
	nd := client.Notifications(secret, WithSyncHandlers(), WithErrorHandler(func(ev Event, err error) {
		if ev.ID != "evt_thin" {
			t.Errorf("error handler got event %q", ev.ID)
		}
		reported = err
	}))
	OnNotification(nd, NotificationInvoicePaid, func(ctx context.Context, n *Notification, cus *Customer) error {
		t.Error("handler must not run with a mismatched object type")
		return nil
	})

	rec := httptest.NewRecorder()
	nd.ServeHTTP(rec, signedRequest(t, secret, thinEvent(string(NotificationInvoicePaid), "")))
	if rec.Code != http.StatusInternalServerError || reported == nil || !strings.Contains(reported.Error(), "not *stripe.Customer") {
		t.Fatalf("got %d / %v", rec.Code, reported)
	}
}

func TestNotificationRejectsBadSignature(t *testing.T) {
	client, _ := newCaptureClient(t, `{}`)
	var reported error
	nd := client.Notifications("whsec_right", WithErrorHandler(func(_ Event, err error) { reported = err }))
	rec := httptest.NewRecorder()
	nd.ServeHTTP(rec, signedRequest(t, "whsec_wrong", thinEvent(string(NotificationInvoicePaid), "")))
	if rec.Code != http.StatusBadRequest || reported == nil {
		t.Fatalf("got %d / %v", rec.Code, reported)
	}
}

// Event types newer than this SDK arrive as UnknownEventNotification; the
// related object is still fetched and decoded into T.
func TestNotificationUnknownEventType(t *testing.T) {
	const secret = "whsec_thin"
	client, cs := newCaptureClient(t, `{"id":"in_1","object":"invoice","status":"open"}`)
	nd := client.Notifications(secret, WithSyncHandlers())
	var status string
	OnNotification(nd, "v1.invoice.brand_new_event", func(ctx context.Context, n *Notification, inv *Invoice) error {
		status = string(inv.Status)
		return nil
	})
	rec := httptest.NewRecorder()
	nd.ServeHTTP(rec, signedRequest(t, secret, thinEvent("v1.invoice.brand_new_event", "")))
	if rec.Code != http.StatusOK || status != "open" {
		t.Fatalf("got %d, status %q", rec.Code, status)
	}
	if r := cs.last(t); r.Path != "/v1/invoices/in_1" {
		t.Fatalf("fetched %s", r.Path)
	}
}
