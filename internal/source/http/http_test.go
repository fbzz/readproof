package http

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fbzz/readproof/internal/source"
)

func TestFetch(t *testing.T) {
	const body = "Products can be refunded within 30 days.\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Fatalf("unexpected Authorization header: got %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "text/markdown")
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer server.Close()

	f := New()
	result, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{
			Kind: source.KindHTTP,
			HTTP: &source.HTTPConfig{
				URL:     server.URL,
				Headers: map[string]string{"Authorization": "Bearer test-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if string(result.Content) != body {
		t.Fatalf("unexpected content: got %q, want %q", string(result.Content), body)
	}
	if result.ContentType != "text/markdown" {
		t.Fatalf("unexpected content type: got %q", result.ContentType)
	}
	if result.SourceRevision != `"abc123"` {
		t.Fatalf("unexpected source revision: got %q, want ETag value", result.SourceRevision)
	}
	if result.Metadata["source_type"] != "http" || result.Metadata["url"] != server.URL {
		t.Fatalf("unexpected metadata: %+v", result.Metadata)
	}
}

// Snapshot provenance is what `readproof diff`'s "why" line and evidence
// exports read, so ETag/Last-Modified must land in Metadata verbatim
// whenever the server sends them — and must be absent, not empty, when it
// doesn't.
func TestFetchRecordsETagAndLastModifiedProvenance(t *testing.T) {
	const lastModified = "Wed, 19 Aug 2026 16:05:30 GMT"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"v3"`)
		w.Header().Set("Last-Modified", lastModified)
		w.Write([]byte("body"))
	}))
	defer server.Close()

	f := New()
	result, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: server.URL}},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := result.Metadata["etag"]; got != `W/"v3"` {
		t.Fatalf("provenance etag = %q, want %q", got, `W/"v3"`)
	}
	if got := result.Metadata["last_modified"]; got != lastModified {
		t.Fatalf("provenance last_modified = %q, want %q", got, lastModified)
	}

	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("body"))
	}))
	defer bare.Close()

	result, err = f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: bare.URL}},
	})
	if err != nil {
		t.Fatalf("fetch (no revision headers): %v", err)
	}
	if _, ok := result.Metadata["etag"]; ok {
		t.Fatalf("etag recorded when the server sent none: %+v", result.Metadata)
	}
	if _, ok := result.Metadata["last_modified"]; ok {
		t.Fatalf("last_modified recorded when the server sent none: %+v", result.Metadata)
	}
}

func TestFetchFallsBackToContentFingerprintWithoutETag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("no revision headers here"))
	}))
	defer server.Close()

	f := New()
	result, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: server.URL}},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.SourceRevision == "" {
		t.Fatalf("expected a non-empty fallback source revision")
	}
}

func TestFetchResolvesEnvVarReferenceHeaders(t *testing.T) {
	t.Setenv("READPROOF_TEST_SECRET_TOKEN", "the-real-secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer the-real-secret"; got != want {
			t.Fatalf("unexpected Authorization header: got %q, want %q", got, want)
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	f := New()
	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{
			Kind: source.KindHTTP,
			HTTP: &source.HTTPConfig{
				URL:     server.URL,
				Headers: map[string]string{"Authorization": "Bearer ${READPROOF_TEST_SECRET_TOKEN}"},
			},
		},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

func TestFetchEnvVarReferenceResolvesEmbeddedInLargerValue(t *testing.T) {
	// "${VAR}" resolves wherever it appears in the header value, not just
	// when it's the entire value — this is what makes "Bearer ${TOKEN}"
	// work, the realistic shape for most auth headers.
	t.Setenv("READPROOF_TEST_SECRET_TOKEN", "the-real-secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("X-Combo"), "prefix-the-real-secret-suffix"; got != want {
			t.Fatalf("unexpected header: got %q, want %q", got, want)
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	f := New()
	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{
			Kind: source.KindHTTP,
			HTTP: &source.HTTPConfig{
				URL:     server.URL,
				Headers: map[string]string{"X-Combo": "prefix-${READPROOF_TEST_SECRET_TOKEN}-suffix"},
			},
		},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

func TestFetchUnresolvedEnvVarReferenceErrors(t *testing.T) {
	f := New()
	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{
			Kind: source.KindHTTP,
			HTTP: &source.HTTPConfig{
				URL:     "http://example.invalid",
				Headers: map[string]string{"Authorization": "${READPROOF_TEST_DEFINITELY_UNSET_VAR}"},
			},
		},
	})
	if err == nil {
		t.Fatalf("expected an error for an unresolved env var reference")
	}
}

// Whoever can register a resource chooses both the target URL and the
// header names, so "${VAR}" expansion is an environment-read primitive
// pointed at an arbitrary endpoint. readproofd's own credentials are the
// worst case — reading the API key would defeat the control that gates
// registration in the first place — so they are refused by name.
func TestFetchRefusesToSendReadproofdsOwnCredentials(t *testing.T) {
	for _, name := range []string{
		"READPROOFD_API_KEY",
		"READPROOF_API_KEY",
		"READPROOFD_POSTGRES_DSN",
		"READPROOFD_S3_ACCESS_KEY",
		"READPROOFD_S3_SECRET_KEY",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "the-servers-own-secret")

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("attacker endpoint was reached with headers %v", r.Header)
				w.Write([]byte("ok"))
			}))
			defer server.Close()

			f := New()
			_, err := f.Fetch(context.Background(), source.FetchRequest{
				Config: source.Config{
					Kind: source.KindHTTP,
					HTTP: &source.HTTPConfig{
						URL:     server.URL,
						Headers: map[string]string{"Authorization": "Bearer ${" + name + "}"},
					},
				},
			})
			if err == nil {
				t.Fatalf("fetch succeeded; want a refusal to expand $%s", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("error %q does not name the refused variable %s", err, name)
			}
		})
	}
}

// The opt-in strict allow-list is the control an operator turns on when
// resource registration is not fully trusted: with it set, only the named
// variables expand and everything else is refused.
func TestFetchEnvAllowlistRestrictsExpansion(t *testing.T) {
	t.Setenv("READPROOF_TEST_ALLOWED", "allowed-value")
	t.Setenv("READPROOF_TEST_DENIED", "denied-value")
	t.Setenv(envAllowlistVar, "READPROOF_TEST_ALLOWED, SOMETHING_ELSE")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Allowed"); got != "allowed-value" {
			t.Errorf("X-Allowed = %q, want %q", got, "allowed-value")
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	f := New()
	if _, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{
			URL:     server.URL,
			Headers: map[string]string{"X-Allowed": "${READPROOF_TEST_ALLOWED}"},
		}},
	}); err != nil {
		t.Fatalf("allow-listed variable was refused: %v", err)
	}

	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{
			URL:     server.URL,
			Headers: map[string]string{"X-Denied": "${READPROOF_TEST_DENIED}"},
		}},
	})
	if err == nil {
		t.Fatalf("fetch succeeded; want a refusal for a variable outside the allow-list")
	}
	if !strings.Contains(err.Error(), envAllowlistVar) {
		t.Fatalf("error %q does not mention %s", err, envAllowlistVar)
	}
}

// readproofd's default: no ${VAR} expands at all, and the refusal names the
// flag that would allow it. Registering such a header is refused up front by
// Validate, which is what turns it into a 400 rather than a later fetch error.
func TestRestrictedEnvRefusesEverythingByDefault(t *testing.T) {
	t.Setenv("READPROOF_TEST_TOKEN", "the-real-secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the endpoint was reached with headers %v", r.Header)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	f := NewWithOptions(Options{RestrictEnv: true})
	cfg := source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{
		URL:     server.URL,
		Headers: map[string]string{"Authorization": "Bearer ${READPROOF_TEST_TOKEN}"},
	}}

	err := f.Validate(cfg)
	if err == nil {
		t.Fatalf("Validate accepted a ${VAR} header with an empty allow-list")
	}
	if !source.IsDenied(err) {
		t.Fatalf("Validate error %v is not a source.DeniedError", err)
	}
	for _, want := range []string{"READPROOF_TEST_TOKEN", "--header-env-allow"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}

	if _, err := f.Fetch(context.Background(), source.FetchRequest{Config: cfg}); err == nil {
		t.Fatalf("fetch expanded a ${VAR} with an empty allow-list")
	}
}

// With an allow-list, exactly the named variables expand and nothing else —
// including a variable that is set in the environment but unlisted, and one
// that is listed but unset.
func TestRestrictedEnvHonoursItsAllowlist(t *testing.T) {
	t.Setenv("READPROOF_TEST_ALLOWED", "allowed-value")
	t.Setenv("READPROOF_TEST_DENIED", "denied-value")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Allowed"); got != "allowed-value" {
			t.Errorf("X-Allowed = %q, want %q", got, "allowed-value")
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	f := NewWithOptions(Options{RestrictEnv: true, EnvAllowlist: []string{"READPROOF_TEST_ALLOWED"}})

	allowed := source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{
		URL:     server.URL,
		Headers: map[string]string{"X-Allowed": "${READPROOF_TEST_ALLOWED}"},
	}}
	if err := f.Validate(allowed); err != nil {
		t.Fatalf("Validate refused an allow-listed variable: %v", err)
	}
	if _, err := f.Fetch(context.Background(), source.FetchRequest{Config: allowed}); err != nil {
		t.Fatalf("fetch refused an allow-listed variable: %v", err)
	}

	denied := source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{
		URL:     server.URL,
		Headers: map[string]string{"X-Denied": "${READPROOF_TEST_DENIED}"},
	}}
	if err := f.Validate(denied); err == nil {
		t.Fatalf("Validate accepted a variable outside the allow-list")
	}
	if _, err := f.Fetch(context.Background(), source.FetchRequest{Config: denied}); err == nil {
		t.Fatalf("fetch expanded a variable outside the allow-list")
	}

	// An allow-listed name that is not set is a different failure — the
	// server's environment does not have it — and must not be silently
	// treated as an empty value.
	unknown := source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{
		URL:     server.URL,
		Headers: map[string]string{"X-Unknown": "${READPROOF_TEST_ALLOWED_BUT_UNSET}"},
	}}
	f.EnvAllowlist = append(f.EnvAllowlist, "READPROOF_TEST_ALLOWED_BUT_UNSET")
	if err := f.Validate(unknown); err != nil {
		t.Fatalf("Validate refused an allow-listed name that happens to be unset: %v", err)
	}
	_, err := f.Fetch(context.Background(), source.FetchRequest{Config: unknown})
	if err == nil {
		t.Fatalf("fetch succeeded for an unset variable")
	}
	if !strings.Contains(err.Error(), "not set") {
		t.Fatalf("error %q does not say the variable is unset", err)
	}
}

// readproofd's own credentials stay refused even when the allow-list names
// them: reading the key that gates registration would defeat the control.
func TestRestrictedEnvStillRefusesOwnCredentials(t *testing.T) {
	t.Setenv("READPROOFD_API_KEY", "the-servers-own-secret")

	f := NewWithOptions(Options{RestrictEnv: true, EnvAllowlist: []string{"READPROOFD_API_KEY"}})
	err := f.Validate(source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{
		URL:     "https://example.invalid/x",
		Headers: map[string]string{"X-Steal": "${READPROOFD_API_KEY}"},
	}})
	if err == nil {
		t.Fatalf("an allow-list entry overrode the deny-list")
	}
	if !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("error %q does not explain the refusal", err)
	}
}

// The embedded CLI is unrestricted on purpose: that environment belongs to
// the person typing the command.
func TestUnrestrictedEnvStillExpands(t *testing.T) {
	t.Setenv("READPROOF_TEST_TOKEN", "the-real-secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer the-real-secret"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	f := New()
	cfg := source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{
		URL:     server.URL,
		Headers: map[string]string{"Authorization": "Bearer ${READPROOF_TEST_TOKEN}"},
	}}
	if err := f.Validate(cfg); err != nil {
		t.Fatalf("the unrestricted fetcher refused a ${VAR} header: %v", err)
	}
	if _, err := f.Fetch(context.Background(), source.FetchRequest{Config: cfg}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

// RP-04. The addresses an SSRF is aiming at, and the ones it is not.
func TestCheckIPRefusesNonPublicAddresses(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1", "127.7.7.7", "::1",
		"169.254.169.254", // the cloud metadata endpoint
		"169.254.0.1", "fe80::1",
		"10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"fc00::1", "fd12:3456::1",
		"100.64.0.1", "100.127.255.255", // RFC 6598 shared address space
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"255.255.255.255",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", // IPv4-mapped IPv6
	} {
		if err := checkIP(net.ParseIP(addr)); err == nil {
			t.Errorf("checkIP(%s) allowed a non-public address", addr)
		}
	}
	for _, addr := range []string{"8.8.8.8", "93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"} {
		if err := checkIP(net.ParseIP(addr)); err != nil {
			t.Errorf("checkIP(%s) refused a public address: %v", addr, err)
		}
	}
}

// On a server, a loopback target is refused before a byte is sent — whether
// it is written as an IP literal or as a name — and the refusal names the
// flag that would allow it.
func TestFetchDeniesPrivateTargetsInServerMode(t *testing.T) {
	reached := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.Write([]byte("ok"))
	}))
	defer server.Close()
	port := server.URL[strings.LastIndex(server.URL, ":")+1:]

	f := NewWithOptions(Options{DenyPrivateTargets: true})
	for _, target := range []string{
		server.URL,
		"http://localhost:" + port + "/",
		"http://api.localhost:" + port + "/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
	} {
		cfg := source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: target}}
		if err := f.Validate(cfg); err == nil {
			t.Errorf("Validate(%s) accepted a private target", target)
		} else if !source.IsDenied(err) {
			t.Errorf("Validate(%s) failed with %v; want a source.DeniedError", target, err)
		} else if !strings.Contains(err.Error(), "--allow-private-sources") {
			t.Errorf("error %q does not name --allow-private-sources", err)
		}
		if _, err := f.Fetch(context.Background(), source.FetchRequest{Config: cfg}); err == nil {
			t.Errorf("Fetch(%s) succeeded against a private target", target)
		}
	}
	if reached {
		t.Fatalf("the loopback server was reached despite the address policy")
	}
}

// The guard has to survive a name that only resolves to a private address at
// connect time — the DNS-rebinding case — which is why it lives in the
// dialer's Control hook and not in a pre-flight lookup.
func TestDialGuardRefusesAtConnectTime(t *testing.T) {
	if err := dialGuard("tcp4", "127.0.0.1:8080", nil); err == nil {
		t.Fatalf("dialGuard allowed a loopback address")
	}
	if err := dialGuard("tcp4", "169.254.169.254:80", nil); err == nil {
		t.Fatalf("dialGuard allowed the metadata endpoint")
	}
	if err := dialGuard("tcp4", "93.184.216.34:80", nil); err != nil {
		t.Fatalf("dialGuard refused a public address: %v", err)
	}

	// And it is actually installed on the client the constructor builds:
	// a request whose host is a name that resolves to loopback still fails.
	f := NewWithOptions(Options{DenyPrivateTargets: true})
	req, err := http.NewRequest(http.MethodGet, "http://localhost.invalid/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if _, err := f.HTTPClient.Transport.RoundTrip(req); err == nil {
		t.Fatalf("the transport connected to an unresolvable/private host")
	}
}

// A redirect is a fresh target chosen by the *source*, so every hop is
// checked — and the chain is capped well below Go's default of 10.
func TestRedirectHopsAreCheckedAndCapped(t *testing.T) {
	client := guardedClient(DefaultTimeout)

	toPrivate, err := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	via := []*http.Request{{}}
	err = client.CheckRedirect(toPrivate, via)
	if err == nil {
		t.Fatalf("a redirect into link-local space was followed")
	}
	if !strings.Contains(err.Error(), "--allow-private-sources") {
		t.Fatalf("error %q does not name --allow-private-sources", err)
	}

	toLoopbackName, _ := http.NewRequest(http.MethodGet, "http://localhost:9999/", nil)
	if err := client.CheckRedirect(toLoopbackName, via); err == nil {
		t.Fatalf("a redirect to a loopback name was followed")
	}

	toFile, _ := http.NewRequest(http.MethodGet, "http://example.com/ok", nil)
	if err := client.CheckRedirect(toFile, via); err != nil {
		t.Fatalf("a redirect to a public address was refused: %v", err)
	}

	longChain := make([]*http.Request, MaxRedirects)
	if err := client.CheckRedirect(toFile, longChain); err == nil {
		t.Fatalf("a chain of %d redirects was not capped", MaxRedirects)
	}
}

// The embedded CLI keeps reaching localhost: developing against a local
// document server (or a local readproofd) is the ordinary case there, and
// redirects between loopback servers keep working.
func TestPrivateTargetsAllowedByDefault(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("final body"))
	}))
	defer final.Close()

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer redirecting.Close()

	f := New()
	result, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: redirecting.URL}},
	})
	if err != nil {
		t.Fatalf("fetch through a loopback redirect: %v", err)
	}
	if string(result.Content) != "final body" {
		t.Fatalf("content = %q, want %q", result.Content, "final body")
	}
}

func TestFetchRejectsNonHTTPSchemes(t *testing.T) {
	f := New()
	for _, raw := range []string{
		"file:///etc/passwd",
		"gopher://127.0.0.1:11211/_stats",
		"ftp://example.com/x",
		"/etc/passwd",
		"example.com/x",
	} {
		_, err := f.Fetch(context.Background(), source.FetchRequest{
			Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: raw}},
		})
		if err == nil {
			t.Errorf("Fetch(%q) succeeded; want a scheme rejection", raw)
		}
	}
}

// A body is buffered whole because the content hash covers all of it, so an
// oversized response must be refused rather than truncated: a truncated
// body would be hashed and stored as if it were the complete document.
func TestFetchRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 4096))
	}))
	defer server.Close()

	f := New()
	f.MaxBytes = 1024
	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: server.URL}},
	})
	if err == nil {
		t.Fatalf("fetch succeeded; want a size-limit error")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error %q does not mention the limit", err)
	}

	// A body exactly on the limit is still accepted.
	exact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 1024))
	}))
	defer exact.Close()
	if _, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: exact.URL}},
	}); err != nil {
		t.Fatalf("a body exactly on the limit was refused: %v", err)
	}
}

func TestFetchTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	f := New()
	f.Timeout = 100 * time.Millisecond
	f.HTTPClient = &http.Client{}

	start := time.Now()
	if _, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: server.URL}},
	}); err == nil {
		t.Fatalf("fetch of a stalled server succeeded; want a timeout")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("fetch took %s; the timeout did not apply", elapsed)
	}
}

func TestFetchNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	f := New()
	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: server.URL}},
	})
	if err == nil {
		t.Fatalf("expected an error for a 404 response")
	}
}
