# kosli-admission-webhook

> [!WARNING]
> Experimental example implementation - not an official Kosli product, not
> production-hardened. See the repository README and TODO.md before use.

Helm chart for a Kubernetes validating admission webhook that asserts pod container images against [Kosli](https://docs.kosli.com/getting_started/enforce_policies) environment policies and rejects non-compliant pods.

## Prerequisites

- Kubernetes 1.25+ (EKS, GKE incl. Autopilot, AKS, OpenShift 4.x, Rancher, k3s, vanilla)
- TLS provider: cert-manager (default), the OpenShift service-ca operator, or a manually provisioned TLS secret and CA bundle
- A Kosli service-account API token
- A Kosli environment with at least one policy attached (or named policies to assert against)

## Install

```bash
kubectl create namespace kosli-system

helm install kosli-webhook ./kosli-admission-webhook \
  --namespace kosli-system \
  --set kosli.org=my-org \
  --set kosli.environment=production \
  --set kosli.existingSecret=kosli-credentials
```

Create the credentials secret out-of-band (or via external-secrets):

```bash
kubectl -n kosli-system create secret generic kosli-credentials \
  --from-literal=api-token="$KOSLI_API_TOKEN"
```

## Recommended rollout

1. Install with defaults: `webhook.failurePolicy=Ignore` (fail open).
2. Optionally start in opt-in mode: `webhook.namespaceSelector.mode=optIn`, then label namespaces `kosli-enforce=true` one at a time.
3. Watch logs and Kosli assert latency for a few days.
4. Flip to strict enforcement:
   ```bash
   helm upgrade kosli-webhook ./kosli-admission-webhook \
     --namespace kosli-system --reuse-values \
     --set webhook.failurePolicy=Fail
   ```

## Key values

| Value | Default | Description |
|---|---|---|
| `kosli.org` | `""` | Kosli organization (required) |
| `kosli.environment` | `""` | Environment whose policies are asserted (leave empty along with `policyNames` for the org default) |
| `kosli.policyNames` | `[]` | Assert named policies instead of an environment (mutually exclusive with `environment`) |
| `kosli.host` | `https://app.kosli.com` | Use `https://app.us.kosli.com` for US orgs |
| `kosli.existingSecret` | `""` | Existing Secret with the API token (preferred) |
| `webhook.failurePolicy` | `Ignore` | `Ignore` = fail open, `Fail` = fail closed |
| `webhook.requireDigestPinning` | `false` | `false` = resolve tag digests from the registry; `true` = deny images not pinned by sha256 digest |
| `webhook.registryCredentialsSecret` | `""` | Existing `kubernetes.io/dockerconfigjson` Secret for resolving tags in private registries |
| `webhook.denyUnknownArtifacts` | `true` | Deny artifacts Kosli has never seen (404) |
| `webhook.cacheTTL` | `60s` | In-memory assert-result cache TTL |
| `webhook.namespaceSelector.mode` | `exclude` | `exclude` or `optIn` |
| `certificates.provider` | `cert-manager` | `cert-manager`, `openshift-service-ca`, or `manual` |
| `podSecurityContext` / `containerSecurityContext` | restricted-PSS defaults, no pinned UID | Fully overridable |
| `platform.gkeAutopilot` | `false` | Omit priorityClassName for Autopilot |
| `proxy.httpsProxy` | `""` | Corporate egress proxy for Kosli API calls |
| `webhook.interceptUpdates` | `false` | Also validate pod UPDATE (ephemeral containers) |
| `webhook.matchConditions` | `[]` | CEL pre-filters on the webhook (K8s 1.27+) |
| `webhook.shutdownDelay` | `5s` | Drain delay after SIGTERM |
| `hostNetwork` | `false` | For clusters where the control plane can't reach pod IPs (e.g. EKS + Calico CNI) |
| `logging.level` / `logging.format` | `info` / `json` | Structured logging |
| `replicaCount` | `2` | Keep >= 2 when failing closed |

## Platform notes

### OpenShift

```bash
helm install kosli-webhook ./kosli-admission-webhook \
  --namespace kosli-system \
  --set certificates.provider=openshift-service-ca \
  --set kosli.org=my-org --set kosli.environment=production \
  --set kosli.existingSecret=kosli-credentials
```

No security-context override is needed: the chart does not pin `runAsUser`, so
the restricted SCC assigns a namespace-range UID while `runAsNonRoot: true`
still enforces non-root (the image declares a numeric non-root USER).
`openshift-service-ca` has the built-in service-ca operator issue the serving
cert (via the Service annotation) and inject the CA bundle into the webhook
config — no cert-manager required.

### GKE private clusters

GKE's auto-created firewall rules only allow the control plane to reach nodes on
443 and 10250. Allow the control-plane CIDR to reach the webhook port, otherwise
every admission call times out:

```bash
gcloud compute firewall-rules create allow-kosli-webhook \
  --network=<cluster-network> \
  --source-ranges=<control-plane-ipv4-cidr> \
  --allow=tcp:8443 \
  --target-tags=<node-tag>
```

### GKE Autopilot

Set `platform.gkeAutopilot=true` (Autopilot rejects `system-cluster-critical`
for user workloads). Autopilot also force-excludes `kube-system` from user
webhooks; the chart excludes it anyway.

### EKS

Works with defaults. With custom/restricted cluster security groups, allow the
control-plane ENIs to reach pods on 8443. Private subnets need NAT or a proxy
(`proxy.httpsProxy`) for egress to the Kosli API.

### AKS

Works with defaults. The AKS admissions enforcer may inject an extra
namespaceSelector term excluding control-plane namespaces — expected, harmless.

## Manual TLS (no cert-manager, no OpenShift)

```bash
helm install kosli-webhook ./kosli-admission-webhook \
  --namespace kosli-system \
  --set certificates.provider=manual \
  --set certificates.manual.secretName=kosli-webhook-tls \
  --set-file certificates.manual.caBundle=ca.b64 \
  ...
```

The TLS secret must be valid for `<fullname>.<namespace>.svc`.

## Safety notes

- The release namespace is always excluded from enforcement to avoid the webhook blocking its own pods.
- `kube-system` is excluded by default; think carefully before removing that exclusion.
- The Deployment sets `priorityClassName: system-cluster-critical` and a PodDisruptionBudget so the webhook survives node drains when failing closed.
- Pair with the [Kosli K8S Reporter](https://docs.kosli.com/helm/k8s_reporter) pointed at the same environment for continuous runtime monitoring.
