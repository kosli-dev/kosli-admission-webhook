# TODO

Deferred items, in priority order. The webhook is functional without these;
they bring it to full parity with reference controllers (cert-manager,
Kyverno, Gatekeeper).

## Metrics & observability

- [ ] Expose Prometheus `/metrics` on a separate plain-HTTP port (`:9090`)
- [ ] `admission_decisions_total{verdict,reason_class}` counter
- [ ] `admission_duration_seconds` histogram
- [ ] `kosli_api_requests_total{code}` counter and `kosli_api_duration_seconds` histogram
- [ ] `assert_cache_hits_total` / `assert_cache_misses_total`
- [ ] Chart: `metrics.enabled`, metrics port on Service, `ServiceMonitor`
      template gated on `metrics.serviceMonitor.enabled`
- [ ] Example Grafana dashboard in `docs/`

## Supply-chain hygiene (dogfooding: this webhook should admit itself)

- [ ] Multi-arch image builds (linux/amd64 + linux/arm64) via buildx
- [ ] cosign keyless signing of released images (`id-token: write` in release.yaml)
- [ ] SBOM generation (syft) attached to the image and the GitHub release
- [ ] Attest builds in a Kosli flow: artifact fingerprint, SBOM, test results —
      so a cluster running this webhook can admit the webhook's own image
- [ ] Pin the webhook's own image by digest in released chart values
- [ ] SLSA provenance for the release workflow

## Small stuff

- [ ] `helm test` hook pod that curls `/healthz` over HTTPS
- [ ] chart-testing (`ct`) + kubeconform against a K8s version matrix in CI
- [ ] Register the webhook under a domain we control (currently
      `kosli-admission-webhook.kosli.com`; change unless this becomes an
      official Kosli project)
- [ ] Artifact Hub metadata + chart icon
- [ ] Unit tests for `internal/admission` (fingerprint parsing, verdict paths)
      and an envtest-based integration test
