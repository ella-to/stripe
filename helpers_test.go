package stripe

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	sgo "github.com/stripe/stripe-go/v86"
)

// captureServer is a fake Stripe API that records every request's form body
// and answers with a minimal JSON object.
type captureServer struct {
	mu   sync.Mutex
	reqs []capturedRequest
}

type capturedRequest struct {
	Method, Path string
	Form         url.Values
}

func newCaptureClient(t *testing.T, response string) (*Client, *captureServer) {
	t.Helper()
	cs := &captureServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cs.mu.Lock()
		cs.reqs = append(cs.reqs, capturedRequest{Method: r.Method, Path: r.URL.Path, Form: r.Form})
		cs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return newTestClient(srv.URL), cs
}

func newTestClient(baseURL string) *Client {
	backends := sgo.NewBackendsWithConfig(&sgo.BackendConfig{
		URL:           sgo.String(baseURL),
		LeveledLogger: &sgo.LeveledLogger{Level: sgo.LevelError},
	})
	return New("sk_test_123", WithStripeClient(sgo.NewClient("sk_test_123", sgo.WithBackends(backends))))
}

func (cs *captureServer) last(t *testing.T) capturedRequest {
	t.Helper()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.reqs) == 0 {
		t.Fatal("no request captured")
	}
	return cs.reqs[len(cs.reqs)-1]
}

func wantForm(t *testing.T, form url.Values, key, want string) {
	t.Helper()
	if got := form.Get(key); got != want {
		t.Errorf("form[%s] = %q, want %q", key, got, want)
	}
}

func wantNoForm(t *testing.T, form url.Values, key string) {
	t.Helper()
	if form.Has(key) {
		t.Errorf("form[%s] = %q, want it unset", key, form.Get(key))
	}
}
