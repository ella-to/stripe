package stripe

import (
	"context"
	"testing"
)

func TestAccountOnboardingLinkOptions(t *testing.T) {
	client, cs := newCaptureClient(t, `{"object":"account_link","url":"https://connect.stripe.test/x"}`)

	link, err := client.AccountOnboardingLink(context.Background(), "acct_1",
		"https://x.test/refresh", "https://x.test/return",
		CollectEventuallyDue(), IncludeFutureRequirements())
	if err != nil {
		t.Fatal(err)
	}
	if link.URL == "" {
		t.Fatal("expected a link url")
	}
	f := cs.last(t).Form
	wantForm(t, f, "account", "acct_1")
	wantForm(t, f, "type", "account_onboarding")
	wantForm(t, f, "collection_options[fields]", "eventually_due")
	wantForm(t, f, "collection_options[future_requirements]", "include")
}

func TestAccountOnboardingLinkDefaults(t *testing.T) {
	client, cs := newCaptureClient(t, `{"object":"account_link","url":"https://connect.stripe.test/x"}`)

	if _, err := client.RequestAccountUpdate(context.Background(), "acct_1", "https://x.test/r", "https://x.test/ok"); err != nil {
		t.Fatal(err)
	}
	f := cs.last(t).Form
	wantForm(t, f, "type", "account_update")
	wantNoForm(t, f, "collection_options[fields]")

	if _, err := client.AccountOnboardingLink(context.Background(), "", "a", "b"); err == nil {
		t.Fatal("expected an error for an empty account id")
	}
}

func TestUpdateTaxRate(t *testing.T) {
	client, cs := newCaptureClient(t, `{"id":"txr_1","object":"tax_rate"}`)

	if _, err := client.ForAccount("acct_1").UpdateTaxRate(context.Background(), "txr_1", TaxRateUpdate{
		DisplayName:  "HST",
		Jurisdiction: "CA - ON",
		Active:       Bool(false),
	}); err != nil {
		t.Fatal(err)
	}
	req := cs.last(t)
	if req.Path != "/v1/tax_rates/txr_1" {
		t.Errorf("path = %q", req.Path)
	}
	wantForm(t, req.Form, "display_name", "HST")
	wantForm(t, req.Form, "jurisdiction", "CA - ON")
	wantForm(t, req.Form, "active", "false")
	wantNoForm(t, req.Form, "description")

	if _, err := client.UpdateTaxRate(context.Background(), "", TaxRateUpdate{}); err == nil {
		t.Fatal("expected an error for an empty id")
	}
}
