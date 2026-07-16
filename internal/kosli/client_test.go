package kosli

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kosli-dev/kosli-admission-webhook/internal/config"
)

// note: TestLogging builds a real slog on a strings.Builder to assert the
// audit lines; everything else discards logs via discardLogger.

const fp = "db7a64bd1d99634842484d1b47c757127052ac0130f397a5ca456a8b23ce21e1"

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestClient points a client at a stub API and returns the request it sent.
func assertAgainst(t *testing.T, cfg config.Config, status int, body string) (*http.Request, cacheResult) {
	t.Helper()
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer srv.Close()
	cfg.Host = srv.URL
	cfg.Org = "kosli-public"
	cfg.Token = "test-token"
	res := New(&cfg, discardLogger()).Assert(fp)
	return got, cacheResult{Allowed: res.Allowed, Reason: res.Reason}
}

type cacheResult struct {
	Allowed bool
	Reason  string
}

func TestRequestShape(t *testing.T) {
	tests := []struct {
		name      string
		cfg       config.Config
		wantQuery string
	}{
		{name: "environment scope", cfg: config.Config{Environment: "production"}, wantQuery: "environment_name=production"},
		{name: "policy scope", cfg: config.Config{PolicyNames: []string{"provenance", "sbom"}}, wantQuery: "policy_name=provenance&policy_name=sbom"},
		{name: "org default (no scope)", cfg: config.Config{}, wantQuery: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := assertAgainst(t, tt.cfg, http.StatusOK, `{"compliant": true}`)
			if want := "/api/v2/asserts/kosli-public/fingerprint/" + fp; req.URL.Path != want {
				t.Errorf("path = %q, want %q", req.URL.Path, want)
			}
			if req.URL.RawQuery != tt.wantQuery {
				t.Errorf("query = %q, want %q", req.URL.RawQuery, tt.wantQuery)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("Authorization = %q", got)
			}
			if got := req.Header.Get("Accept"); got != "application/json" {
				t.Errorf("Accept = %q", got)
			}
			if got := req.Header.Get("User-Agent"); got != "kosli-admission-webhook" {
				t.Errorf("User-Agent = %q", got)
			}
		})
	}
}

func TestVerdicts(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		denyUnkn   bool
		wantAllow  bool
		wantReason string // substring
	}{
		{name: "compliant", status: 200, body: `{"compliant": true}`, wantAllow: true},
		{
			name:   "non-compliant policy and attestation",
			status: 200,
			body: `{"compliant": false,
				"policy_evaluations": [{"policy_name": "sbom", "status": "NON_COMPLIANT"}],
				"compliance_status": {"attestations_statuses": [{"attestation_name": "unit-tests", "is_compliant": false}]}}`,
			wantReason: "non-compliant: policy sbom, attestation unit-tests",
		},
		{name: "unknown artifact denied", status: 404, body: `{"message": "not found"}`, denyUnkn: true, wantReason: "artifact unknown"},
		{name: "unknown artifact allowed", status: 404, body: `{"message": "not found"}`, wantAllow: true},
		{name: "auth failure", status: 401, body: `{}`, wantReason: "auth failed"},
		{name: "server error", status: 500, body: `boom`, wantReason: "returned 500"},
		{name: "garbage body", status: 200, body: `{{{`, wantReason: "cannot parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, res := assertAgainst(t, config.Config{Environment: "e", DenyUnknownArtifacts: tt.denyUnkn}, tt.status, tt.body)
			if res.Allowed != tt.wantAllow {
				t.Errorf("Allowed = %v, want %v (reason %q)", res.Allowed, tt.wantAllow, res.Reason)
			}
			if !strings.Contains(res.Reason, tt.wantReason) {
				t.Errorf("Reason = %q, want substring %q", res.Reason, tt.wantReason)
			}
		})
	}
}

func TestLogging(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"compliant": false, "policy_evaluations": [{"policy_name": "sbom", "status": "NON_COMPLIANT"}]}`))
	}))
	defer srv.Close()
	c := New(&config.Config{Host: srv.URL, Org: "o", Token: "secret-token", Environment: "e", CacheTTL: time.Minute}, log)
	c.Assert(fp) // API verdict -> request debug line + result info line
	c.Assert(fp) // cache hit -> cached debug line

	out := buf.String()
	for _, want := range []string{
		`msg="kosli assert request"`,
		"environment_name=e",
		`msg="kosli assert result" fingerprint=` + fp + " allowed=false",
		"policy sbom",
		`msg="kosli assert result (cached)"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret-token") {
		t.Error("log output leaks the API token")
	}
}

func TestTransportErrorDenies(t *testing.T) {
	cfg := &config.Config{Host: "http://127.0.0.1:1", Org: "o", Token: "t", Environment: "e"}
	res := New(cfg, discardLogger()).Assert(fp)
	if res.Allowed || !strings.Contains(res.Reason, "unreachable") {
		t.Fatalf("got Allowed=%v Reason=%q", res.Allowed, res.Reason)
	}
}

func TestVerdictsAreCached(t *testing.T) {
	// definitive verdicts (200 and 404) are served from cache
	for _, tt := range []struct{ status int }{{200}, {404}} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.WriteHeader(tt.status)
			w.Write([]byte(`{"compliant": true}`))
		}))
		c := New(&config.Config{Host: srv.URL, Org: "o", Token: "t", Environment: "e", CacheTTL: time.Minute, DenyUnknownArtifacts: true}, discardLogger())
		c.Assert(fp)
		c.Assert(fp)
		srv.Close()
		if calls != 1 {
			t.Errorf("status %d: API called %d times, want 1 (cached)", tt.status, calls)
		}
	}
}

func TestFailuresAreNotCached(t *testing.T) {
	// a failure to obtain a verdict denies but is retried on the next
	// admission: here the API recovers after one 500 and the second
	// Assert must reach it and allow
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"compliant": true}`))
	}))
	defer srv.Close()
	c := New(&config.Config{Host: srv.URL, Org: "o", Token: "t", Environment: "e", CacheTTL: time.Minute}, discardLogger())
	if res := c.Assert(fp); res.Allowed {
		t.Fatal("first Assert should deny on 500")
	}
	if res := c.Assert(fp); !res.Allowed {
		t.Fatalf("second Assert should retry and allow, got deny: %s", res.Reason)
	}
	if calls != 2 {
		t.Fatalf("API called %d times, want 2 (failure not cached)", calls)
	}
}
