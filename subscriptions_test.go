package stripe

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestPauseSubscription(t *testing.T) {
	client, cs := newCaptureClient(t, `{"id":"sub_1","object":"subscription","status":"paused"}`)
	sub, err := client.PauseSubscription(context.Background(), "sub_1")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Status != SubscriptionPaused || HasAccess(sub) {
		t.Fatalf("got status %q, HasAccess=%v", sub.Status, HasAccess(sub))
	}
	if r := cs.last(t); r.Method != http.MethodPost || r.Path != "/v1/subscriptions/sub_1/pause" {
		t.Fatalf("got %s %s", r.Method, r.Path)
	}
}

func TestSubscriptionPauseReason(t *testing.T) {
	var sub Subscription
	raw := `{"id":"sub_1","status":"paused","status_details":{"paused":{"type":"subscription","transitioned_at":1767225600,"subscription":{"type":"trial_end_without_payment_method"}}}}`
	if err := json.Unmarshal([]byte(raw), &sub); err != nil {
		t.Fatal(err)
	}
	reason, since, ok := SubscriptionPauseReason(&sub)
	if !ok || reason != PausedTrialEndedWithoutCard || since.Unix() != 1767225600 {
		t.Fatalf("got %q %v %v", reason, since, ok)
	}

	if _, _, ok := SubscriptionPauseReason(&Subscription{Status: SubscriptionActive}); ok {
		t.Error("active subscription reported a pause reason")
	}
	if _, _, ok := SubscriptionPauseReason(&Subscription{Status: SubscriptionPaused}); ok {
		t.Error("paused subscription without status_details reported a reason")
	}
	if _, _, ok := SubscriptionPauseReason(nil); ok {
		t.Error("nil subscription reported a reason")
	}
}
