// Package kosli implements the assert-API client.
package kosli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kosli-dev/kosli-admission-webhook/internal/cache"
	"github.com/kosli-dev/kosli-admission-webhook/internal/config"
)

type Client struct {
	cfg   *config.Config
	http  *http.Client
	cache *cache.TTL
	log   *slog.Logger
}

func New(cfg *config.Config, log *slog.Logger) *Client {
	return &Client{
		cfg:   cfg,
		http:  &http.Client{Timeout: 8 * time.Second},
		cache: cache.New(cfg.CacheTTL),
		log:   log,
	}
}

type assertResponse struct {
	Compliant         bool `json:"compliant"`
	PolicyEvaluations []struct {
		PolicyName string `json:"policy_name"`
		Status     string `json:"status"`
	} `json:"policy_evaluations"`
	ComplianceStatus *struct {
		AttestationsStatuses []struct {
			AttestationName string `json:"attestation_name"`
			IsCompliant     *bool  `json:"is_compliant"`
		} `json:"attestations_statuses"`
	} `json:"compliance_status"`
}

// Assert checks a fingerprint against the configured Kosli scope, with
// caching. Only definitive verdicts (compliant, non-compliant, unknown
// artifact) are cached; failures to obtain a verdict — timeouts,
// unreachable API, auth errors, 5xx, unparseable bodies — still deny but
// are retried on the next admission instead of pinning the denial for a
// whole TTL.
func (k *Client) Assert(fingerprint string) cache.Result {
	if r, ok := k.cache.Get(fingerprint); ok {
		k.log.Debug("kosli assert result (cached)", "fingerprint", fingerprint, "allowed", r.Allowed, "reason", r.Reason)
		return r
	}
	r, definitive := k.assertUncached(fingerprint)
	if definitive {
		// failures are logged at ERROR where they occur; this is the
		// compliance audit line for every verdict the API actually gave
		k.log.Info("kosli assert result", "fingerprint", fingerprint, "allowed", r.Allowed, "reason", r.Reason)
		k.cache.Set(fingerprint, r)
	}
	return r
}

func (k *Client) assertUncached(fp string) (cache.Result, bool) {
	u, _ := url.Parse(fmt.Sprintf("%s/api/v2/asserts/%s/fingerprint/%s",
		k.cfg.Host, url.PathEscape(k.cfg.Org), url.PathEscape(fp)))
	q := u.Query()
	if k.cfg.Environment != "" {
		q.Set("environment_name", k.cfg.Environment)
	}
	for _, p := range k.cfg.PolicyNames {
		q.Add("policy_name", p)
	}
	u.RawQuery = q.Encode()

	// the URL carries no secrets (auth is a header), so it is safe to log
	k.log.Debug("kosli assert request", "url", u.String())

	req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
	req.Header.Set("Authorization", "Bearer "+k.cfg.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "kosli-admission-webhook")

	resp, err := k.http.Do(req)
	if err != nil {
		// transport error: no verdict obtained, deny with reason.
		k.log.Error("Kosli API request failed", "fingerprint", fp, "error", err)
		return cache.Result{Allowed: false, Reason: "Kosli API unreachable: " + err.Error()}, false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusNotFound:
		// normal outcome (artifact without provenance), not an API failure
		if k.cfg.DenyUnknownArtifacts {
			return cache.Result{Allowed: false, Reason: "artifact unknown to Kosli (no provenance recorded)"}, true
		}
		return cache.Result{Allowed: true}, true
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		k.log.Error("Kosli API auth failed", "fingerprint", fp, "status", resp.StatusCode, "body", bodySnippet(body))
		return cache.Result{Allowed: false, Reason: "Kosli auth failed (check API token)"}, false
	case resp.StatusCode != http.StatusOK:
		k.log.Error("Kosli API returned unexpected status", "fingerprint", fp, "status", resp.StatusCode, "body", bodySnippet(body))
		return cache.Result{Allowed: false, Reason: fmt.Sprintf("Kosli API returned %d", resp.StatusCode)}, false
	}

	var ar assertResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		k.log.Error("cannot parse Kosli API response", "fingerprint", fp, "error", err, "body", bodySnippet(body))
		return cache.Result{Allowed: false, Reason: "cannot parse Kosli response: " + err.Error()}, false
	}
	if ar.Compliant {
		return cache.Result{Allowed: true}, true
	}
	return cache.Result{Allowed: false, Reason: nonComplianceReason(&ar)}, true
}

// bodySnippet bounds response bodies for log lines.
func bodySnippet(body []byte) string {
	const max = 512
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func nonComplianceReason(ar *assertResponse) string {
	var parts []string
	for _, pe := range ar.PolicyEvaluations {
		if pe.Status != "COMPLIANT" {
			parts = append(parts, "policy "+pe.PolicyName)
		}
	}
	if ar.ComplianceStatus != nil {
		for _, as := range ar.ComplianceStatus.AttestationsStatuses {
			if as.IsCompliant == nil || !*as.IsCompliant {
				parts = append(parts, "attestation "+as.AttestationName)
			}
		}
	}
	if len(parts) == 0 {
		return "artifact is non-compliant"
	}
	return "non-compliant: " + strings.Join(parts, ", ")
}
