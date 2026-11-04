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

func TestCreateTrialOffer(t *testing.T) {
	client, cs := newCaptureClient(t, `{"id":"trof_1","object":"product_catalog.trial_offer"}`)
	if _, err := client.CreateTrialOffer(context.Background(), TrialOfferParams{
		TrialPriceID: "price_intro", Intervals: 3, ThenPriceID: "price_pro", Nickname: "spring",
	}); err != nil {
		t.Fatal(err)
	}
	r := cs.last(t)
	if r.Path != "/v1/product_catalog/trial_offers" {
		t.Fatalf("got path %s", r.Path)
	}
	wantForm(t, r.Form, "price", "price_intro")
	wantForm(t, r.Form, "duration[type]", "relative")
	wantForm(t, r.Form, "duration[relative][iterations]", "3")
	wantForm(t, r.Form, "end_behavior[transition][price]", "price_pro")
	wantForm(t, r.Form, "nickname", "spring")

	if _, err := client.CreateTrialOffer(context.Background(), TrialOfferParams{TrialPriceID: "p"}); err == nil {
		t.Error("expected an error for missing ThenPriceID/Intervals")
	}
}

func TestSubscribeWithTrialOffer(t *testing.T) {
	client, cs := newCaptureClient(t, `{"id":"sub_1","object":"subscription"}`)
	if _, err := client.Subscribe(context.Background(), SubscribeParams{
		Customer: "cus_1", PriceID: "price_intro", TrialOfferID: "trof_1",
	}); err != nil {
		t.Fatal(err)
	}
	f := cs.last(t).Form
	wantForm(t, f, "items[0][price]", "price_intro")
	wantForm(t, f, "items[0][current_trial][trial_offer]", "trof_1")
	wantNoForm(t, f, "trial_period_days")

	if _, err := client.Subscribe(context.Background(), SubscribeParams{
		Customer: "cus_1", PriceID: "price_intro", TrialOfferID: "trof_1", TrialDays: 7,
	}); err == nil {
		t.Error("expected an error when combining TrialOfferID and TrialDays")
	}
}
