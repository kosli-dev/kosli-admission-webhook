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
make docker IMAGE=ghcr.io/kosli-dev/kosli-webhook TAG=1.0.0
helm install kosli-webhook charts/kosli-admission-webhook \
  -n kosli-system --create-namespace \
  --set kosli.org=my-org --set kosli.environment=production \
  --set kosli.existingSecret=kosli-credentials
```

## Design notes

- Pods must reference images by digest (`repo@sha256:...`); the digest is the
  Kosli fingerprint. Configurable via `REQUIRE_DIGEST_PINNING`.
- Transport errors and unknown artifacts deny with an explicit reason rather
  than surfacing as webhook errors, so behavior is deterministic regardless of
  `failurePolicy`.
- Graceful shutdown: SIGTERM flips `/readyz` to 503, waits `SHUTDOWN_DELAY`,
  then drains in-flight admission reviews.
- Pair with the [Kosli K8S Reporter](https://docs.kosli.com/helm/k8s_reporter)
  for continuous runtime monitoring of the same environment.
