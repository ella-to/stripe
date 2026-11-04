package stripe

import (
	"context"
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
