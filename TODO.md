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

## Tag resolution: latency & correctness (evaluated, deferred)

Tags are resolved to sha256 digests via a registry HEAD request at
admission time (see `internal/resolver`, cached per `CACHE_TTL`). If that
round-trip ever becomes a latency or rate-limit problem, options in order
of preference:

- [ ] Mutate pods to the resolved digest (MutatingWebhookConfiguration +
      JSONPatch, `webhook.mutateToDigest` value). Primarily a *correctness*
      fix — closes the tag-mutability TOCTOU gap entirely — but also helps
      latency indirectly: once mutated, downstream controllers re-submit
      digest-pinned specs that need no resolution.
- [ ] Registry pull-through cache / mirror (infra-level, no code change) —
      the standard answer to registry latency and Docker Hub rate limits;
      the HEAD request is a few KB and cached, so keep the current design
      until it actually hurts.
- [ ] `node.status.images` fast path: informer over nodes builds a
      tag→digest map from images kubelet already reported, falling back to
      the registry on miss. Zero external calls on hit, but: requires
      re-enabling the service account token + nodes RBAC (currently
      `automountServiceAccountToken: false`), adds client-go, the list is
      truncated (~50 images/node by default), and different nodes may hold
      different digests for the same tag — best-effort by construction.
- [x] Private registries: resolution uses the Docker keychain; the chart's
      `webhook.registryCredentialsSecret` mounts an existing
      dockerconfigjson Secret and sets `DOCKER_CONFIG`. Reading pod
      `imagePullSecrets` instead would need K8s API access the webhook
      deliberately doesn't have — still deferred.

## Small stuff

- [ ] `helm test` hook pod that curls `/healthz` over HTTPS
- [ ] chart-testing (`ct`) + kubeconform against a K8s version matrix in CI
- [ ] Register the webhook under a domain we control (currently
      `kosli-admission-webhook.kosli.com`; change unless this becomes an
      official Kosli project)
- [ ] Artifact Hub metadata + chart icon
- [ ] Unit tests for `internal/admission` verdict paths and an envtest-based
      integration test (fingerprint parsing and `internal/resolver` are
      covered)
