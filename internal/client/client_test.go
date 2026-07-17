package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRetriesOn429HonoringRetryAfter(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"Too many requests."}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"abc","name":"ok","check_type":"http","status":"up","is_paused":false}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "lvk_test")
	svc, err := c.GetService(context.Background(), "abc")
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if svc.Name != "ok" {
		t.Fatalf("unexpected service: %+v", svc)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected exactly one retry (2 calls), got %d", calls.Load())
	}
}

// A 5xx may arrive after the server already committed the write, and the create
// endpoints take no idempotency key, so a replayed POST would silently leave a
// second resource behind that no state refers to.
func TestCreateIsNotRetriedOn5xx(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"Server Error"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "lvk_test")
	if _, err := c.CreateService(context.Background(), ServiceInput{Name: "x", CheckType: "http"}); err == nil {
		t.Fatal("expected the 500 to surface as an error")
	}
	if calls.Load() != 1 {
		t.Fatalf("a create must be sent exactly once, got %d calls", calls.Load())
	}
}

// A 429 is a rejection: the server did not act on the request, so replaying it
// cannot duplicate anything, even for a create.
func TestCreateIsStillRetriedOn429(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"Too many requests."}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"abc","name":"x","check_type":"http","status":"unknown","is_paused":false}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "lvk_test")
	svc, err := c.CreateService(context.Background(), ServiceInput{Name: "x", CheckType: "http"})
	if err != nil {
		t.Fatalf("expected the 429 to be retried, got %v", err)
	}
	if svc.ID != "abc" {
		t.Fatalf("unexpected service: %+v", svc)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected exactly one retry (2 calls), got %d", calls.Load())
	}
}

// Guards the other half of the policy: idempotent methods keep retrying 5xx.
func TestIdempotentRequestIsStillRetriedOn5xx(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"abc","name":"ok","check_type":"http","status":"up","is_paused":false}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "lvk_test")
	if _, err := c.GetService(context.Background(), "abc"); err != nil {
		t.Fatalf("expected the 502 to be retried, got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected exactly one retry (2 calls), got %d", calls.Load())
	}
}

func TestNotFoundIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"The requested resource was not found."}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "lvk_test")
	_, err := c.GetService(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestValidationErrorCarriesFieldMap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"The given data was invalid.","errors":{"settings.interval_seconds":["Below the plan minimum."]}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "lvk_test")
	_, err := c.CreateService(context.Background(), ServiceInput{Name: "x", CheckType: "http"})

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %v", err)
	}
	if len(ve.Errors["settings.interval_seconds"]) != 1 {
		t.Fatalf("field error map not decoded: %+v", ve)
	}
}

func TestAuthHeaderAndPathAreSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer lvk_test" {
			t.Errorf("missing bearer token, got %q", got)
		}
		if r.Header.Get("Accept-Language") != "" {
			t.Errorf("Accept-Language must not be sent (locale-stable reads)")
		}
		if r.URL.Path != "/v1/statuspages/page1" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"page1","name":"n","slug":"s","is_published":true,"access_type":"public"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "lvk_test")
	if _, err := c.GetStatuspage(context.Background(), "page1"); err != nil {
		t.Fatal(err)
	}
}

func TestMergeSecretsReplacesSentinelsRecursively(t *testing.T) {
	remote := json.RawMessage(`{
		"method": "POST",
		"headers": {"X-Api-Key": "__LIVCK_KEEP_UNCHANGED__", "Accept": "__LIVCK_KEEP_UNCHANGED__"},
		"auth": {"type": "bearer", "token": "__LIVCK_KEEP_UNCHANGED__"},
		"conditions": [{"field": "status_code", "operator": "gte", "value": 400, "status": "down"}]
	}`)
	prior := json.RawMessage(`{
		"method": "POST",
		"headers": {"X-Api-Key": "secret-key", "Accept": "application/json"},
		"auth": {"type": "bearer", "token": "secret-token"}
	}`)

	merged, err := MergeSecrets(remote, prior)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if err := json.Unmarshal(merged, &out); err != nil {
		t.Fatal(err)
	}

	headers := out["headers"].(map[string]any)
	if headers["X-Api-Key"] != "secret-key" || headers["Accept"] != "application/json" {
		t.Fatalf("header secrets not merged: %v", headers)
	}
	auth := out["auth"].(map[string]any)
	if auth["token"] != "secret-token" {
		t.Fatalf("auth secret not merged: %v", auth)
	}
	if out["method"] != "POST" {
		t.Fatalf("non-secret field mangled: %v", out["method"])
	}
	if _, ok := out["conditions"].([]any); !ok {
		t.Fatalf("conditions lost in merge")
	}
}

func TestMergeSecretsKeepsSentinelWithoutPrior(t *testing.T) {
	remote := json.RawMessage(`{"auth":{"type":"bearer","token":"__LIVCK_KEEP_UNCHANGED__"}}`)

	merged, err := MergeSecrets(remote, nil)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	_ = json.Unmarshal(merged, &out)
	if out["auth"].(map[string]any)["token"] != KeepSentinel {
		t.Fatalf("sentinel must survive when no prior value exists (import case)")
	}
}

func TestReconcileConfigKeepsUserJSONWhenSubsetOfSeededEcho(t *testing.T) {
	prior := json.RawMessage(`{"method":"GET","conditions":[{"field":"status_code","operator":"gte","value":400,"status":"down"}]}`)
	// Server echo: user's values + seeded defaults + sentinel-masked header
	remote := json.RawMessage(`{
		"method":"GET","follow_redirects":true,"verify_ssl":true,"ip_version":"auto",
		"conditions":[{"field":"status_code","operator":"gte","value":400,"status":"down"}]
	}`)

	got, err := ReconcileConfig(remote, prior)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(prior) {
		t.Fatalf("expected the user's exact JSON to be kept, got %s", got)
	}
}

func TestReconcileConfigSurfacesRealRemoteDrift(t *testing.T) {
	prior := json.RawMessage(`{"method":"GET"}`)
	remote := json.RawMessage(`{"method":"POST","verify_ssl":true}`)

	got, err := ReconcileConfig(remote, prior)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	_ = json.Unmarshal(got, &out)
	if out["method"] != "POST" {
		t.Fatalf("expected remote drift to surface, got %s", got)
	}
}

func TestReconcileConfigNullPriorStaysUnmanaged(t *testing.T) {
	remote := json.RawMessage(`{"method":"GET","verify_ssl":true}`)

	got, err := ReconcileConfig(remote, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("unmanaged config must stay null, got %s", got)
	}
}
