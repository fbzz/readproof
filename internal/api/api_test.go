package api_test

import (
	"net/http/httptest"
	"testing"

	"ctx/internal/api"
	"ctx/internal/app"
	"ctx/internal/client/remote"
	"ctx/internal/policy"
	"ctx/internal/resource"
	"ctx/internal/source"
)

func TestHTTPHeaderCredentialsAreRedactedInResponses(t *testing.T) {
	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	server := httptest.NewServer(api.NewHandler(a, api.Options{}))
	defer server.Close()

	c := remote.New(server.URL, "")
	defer c.Close()

	ctx := t.Context()
	uri := "ctx://demo/policies/secret-header"

	err = c.RegisterResource(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/secret-header",
		SourceConfig: source.Config{
			Kind: source.KindHTTP,
			HTTP: &source.HTTPConfig{
				URL: "https://example.invalid/refunds.md",
				Headers: map[string]string{
					"Authorization": "Bearer super-secret-value",
					"Accept":        "text/markdown",
				},
			},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	})
	if err != nil {
		t.Fatalf("register resource: %v", err)
	}

	got, err := c.GetResource(ctx, uri)
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	if got.SourceConfig.HTTP == nil {
		t.Fatalf("expected HTTP source config, got nil")
	}
	if got.SourceConfig.HTTP.Headers["Authorization"] != "[REDACTED]" {
		t.Fatalf("expected Authorization header to be redacted in the API response, got %q", got.SourceConfig.HTTP.Headers["Authorization"])
	}
	if got.SourceConfig.HTTP.Headers["Accept"] != "text/markdown" {
		t.Fatalf("expected non-sensitive Accept header to pass through unredacted, got %q", got.SourceConfig.HTTP.Headers["Accept"])
	}

	resources, err := c.ListResources(ctx)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].SourceConfig.HTTP.Headers["Authorization"] != "[REDACTED]" {
		t.Fatalf("expected Authorization header to be redacted in the list response, got %q", resources[0].SourceConfig.HTTP.Headers["Authorization"])
	}
}
