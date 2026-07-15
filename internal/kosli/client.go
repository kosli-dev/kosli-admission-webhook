// Package kosli implements the assert-API client.
package kosli

import (
	"encoding/json"
	"fmt"
	"io"
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
}

func New(cfg *config.Config) *Client {
	return &Client{
		cfg:   cfg,
		http:  &http.Client{Timeout: 8 * time.Second},
		cache: cache.New(cfg.CacheTTL),
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

// Assert checks a fingerprint against the configured Kosli scope, with caching.
func (k *Client) Assert(fingerprint string) cache.Result {
	if r, ok := k.cache.Get(fingerprint); ok {
		return r
	}
	r := k.assertUncached(fingerprint)
	k.cache.Set(fingerprint, r)
	return r
}

func (k *Client) assertUncached(fp string) cache.Result {
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

	req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
	req.Header.Set("Authorization", "Bearer "+k.cfg.Token)

	resp, err := k.http.Do(req)
	if err != nil {
		// transport error: no verdict obtained, deny with reason.
		return cache.Result{Allowed: false, Reason: "Kosli API unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusNotFound:
		if k.cfg.DenyUnknownArtifacts {
			return cache.Result{Allowed: false, Reason: "artifact unknown to Kosli (no provenance recorded)"}
		}
		return cache.Result{Allowed: true}
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return cache.Result{Allowed: false, Reason: "Kosli auth failed (check API token)"}
	case resp.StatusCode != http.StatusOK:
		return cache.Result{Allowed: false, Reason: fmt.Sprintf("Kosli API returned %d", resp.StatusCode)}
	}

	var ar assertResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return cache.Result{Allowed: false, Reason: "cannot parse Kosli response: " + err.Error()}
	}
	if ar.Compliant {
		return cache.Result{Allowed: true}
	}
	return cache.Result{Allowed: false, Reason: nonComplianceReason(&ar)}
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
