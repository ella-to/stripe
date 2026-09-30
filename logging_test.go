package stripe

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sgo "github.com/stripe/stripe-go/v86"
)

func TestStripeGoLogsGoThroughSlog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad things","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c := &Client{logger: logger}
	backends := sgo.NewBackendsWithConfig(&sgo.BackendConfig{
		URL:           sgo.String(srv.URL),
		LeveledLogger: slogAdapter{logger: c.log},
	})
	c.api = sgo.NewClient("sk_test_123", sgo.WithBackends(backends))

	if _, err := c.GetCustomer(context.Background(), "cus_1"); err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(buf.String(), "stripe-go:") || !strings.Contains(buf.String(), "level=DEBUG") {
		t.Fatalf("stripe-go log not routed through slog at debug level:\n%s", buf.String())
	}
}

func TestWebhooksInheritClientLogger(t *testing.T) {
	var buf bytes.Buffer
	c := New("sk_test_123", WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))
	d := c.Webhooks() // no secret configured
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{}")))
	if !strings.Contains(buf.String(), "webhook secret is not configured") {
		t.Fatalf("expected the dispatcher to log via the client logger, got %q", buf.String())
	}
}
