[![test](https://github.com/kosli-dev/kosli-admission-webhook/actions/workflows/test.yaml/badge.svg)](https://github.com/kosli-dev/kosli-admission-webhook/actions/workflows/test.yaml)

# kosli-admission-webhook

> [!WARNING]
> **Experimental - example implementation.** This project demonstrates how to
> build a Kosli policy-enforcement admission webhook. It has been validated at
> the build/lint/template level only and has **not** been battle-tested in
> production clusters. It is not an official Kosli product and comes with no
> support or stability guarantees. Review the code, run it with
> `failurePolicy: Ignore` in a non-production cluster first, and see
> [TODO.md](TODO.md) for known gaps (no metrics, no signed releases, no test
> suite) before considering any production use.

A Kubernetes validating admission webhook that asserts pod container images
against [Kosli](https://docs.kosli.com/getting_started/enforce_policies)
environment policies and rejects non-compliant pods at admission time.

- **Binary**: `cmd/webhook` (stateless HTTPS server, no Kubernetes API access)
- **Chart**: `charts/kosli-admission-webhook` — see its README for install,
  platform notes (EKS, GKE/Autopilot, AKS, OpenShift), and all values
- **Deferred work**: see [TODO.md](TODO.md)

> Community project — not affiliated with Kosli.

## Quick start

```bash
make all            # vet, test, build, helm-lint
make e2e            # kind-based end-to-end test (make e2e-clean tears down
                    # a cluster kept by KEEP=1/KEEP_ON_FAIL=1)
make docker IMAGE=ghcr.io/kosli-dev/kosli-webhook TAG=1.0.0
helm install kosli-webhook charts/kosli-admission-webhook \
  -n kosli-system --create-namespace \
  --set kosli.org=my-org --set kosli.environment=production \
  --set kosli.existingSecret=kosli-credentials
```

## Design notes

- Any image reference works: digest-pinned images (`repo@sha256:...`) use the
  digest directly, and plain tags are resolved to their sha256 digest via a
  registry manifest HEAD request at admission time. The digest is the Kosli
  fingerprint. Auth uses the Docker keychain: public registries work
  anonymously; for private registries point the chart's
  `webhook.registryCredentialsSecret` at an existing dockerconfigjson
  Secret. Caveat: tags are mutable, so the verdict applies to what
  the tag pointed at during admission — a moved tag or a node-cached image
  under `imagePullPolicy: IfNotPresent` can diverge from the asserted
  digest. Set `REQUIRE_DIGEST_PINNING=true` to deny unpinned images outright
  — the strict-guarantee mode.
- Registry digest resolutions are cached with the same TTL as assert results
  (`CACHE_TTL`), so a hot tag costs one HEAD request per TTL window.
- Transport errors and unknown artifacts deny with an explicit reason rather
  than surfacing as webhook errors, so behavior is deterministic regardless of
  `failurePolicy`. Only definitive verdicts (compliant, non-compliant,
  unknown artifact) are cached; API failures deny but are retried on the
  next admission, so recovery is immediate once the API is reachable again.
- Logging: every Kosli verdict (compliant or not) is an info-level
  `kosli assert result` line, every tag resolution a `resolved tag to
  digest` line, and every admission decision an `admission decision`
  line; API failures log at error with status and body. `LOG_LEVEL=debug`
  adds assert request URLs and cache hits.
- Graceful shutdown: SIGTERM flips `/readyz` to 503, waits `SHUTDOWN_DELAY`,
  then drains in-flight admission reviews.
- Pair with the [Kosli K8S Reporter](https://docs.kosli.com/helm/k8s_reporter)
  for continuous runtime monitoring of the same environment.
