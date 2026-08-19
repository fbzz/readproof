package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"ctx/internal/api"
	"ctx/internal/app"
	"ctx/internal/client/remote"
)

func TestAPIKeyRequiredWhenConfigured(t *testing.T) {
	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	server := httptest.NewServer(api.NewHandler(a, api.Options{APIKey: "correct-key"}))
	defer server.Close()

	// /healthz must remain reachable without a key (container healthchecks
	// need it).
	resp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /healthz to be reachable without a key, got %d", resp.StatusCode)
	}

	// Everything else must reject a missing/wrong key.
	resp, err = http.Get(server.URL + "/v1/resources")
	if err != nil {
		t.Fatalf("list resources (no key): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no key, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/resources", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list resources (wrong key): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a wrong key, got %d", resp.StatusCode)
	}

	// The remote client, given the right key, works normally.
	c := remote.New(server.URL, "correct-key")
	defer c.Close()
	if _, err := c.ListResources(t.Context()); err != nil {
		t.Fatalf("list resources with correct key: %v", err)
	}

	// The remote client, given no key at all, gets the 401 surfaced as an error.
	unauthed := remote.New(server.URL, "")
	defer unauthed.Close()
	if _, err := unauthed.ListResources(t.Context()); err == nil {
		t.Fatalf("expected an error when calling an API-key-protected server without a key")
	}
}

func TestNoAPIKeyMeansUnauthenticated(t *testing.T) {
	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	server := httptest.NewServer(api.NewHandler(a, api.Options{}))
	defer server.Close()

	c := remote.New(server.URL, "")
	defer c.Close()
	if _, err := c.ListResources(t.Context()); err != nil {
		t.Fatalf("expected no auth required by default, got: %v", err)
	}
}
