#!/usr/bin/env bash
# End-to-end test of the Kosli admission webhook on a local kind cluster,
# using an in-cluster mock of the Kosli assert API (no credentials needed).
#
# Prereqs: docker, kind, kubectl, helm
# Usage:   ./hack/kind-e2e.sh                 # create cluster (or reuse an
#                                             # existing kosli-e2e one), run
#                                             # tests, clean up
#          FRESH=1 ./hack/kind-e2e.sh         # delete an existing kosli-e2e
#                                             # cluster and start clean
#          KEEP=1 ./hack/kind-e2e.sh          # keep cluster + images for inspection
#          KEEP_ON_FAIL=1 ./hack/kind-e2e.sh  # clean up on success, but keep the
#                                             # cluster for debugging when anything
#                                             # fails (handy in CI-triage loops)
#          SHOW_LOGS=1 ./hack/kind-e2e.sh     # print the webhook's decision log
#                                             # lines after the assertions
#          CLEAN_IMAGES=1 ./hack/kind-e2e.sh  # also remove the cached kindest/node
#                                             # image — frees ~900MB, slows next run
#
# LIVE mode — test against the real Kosli API instead of the mock:
#          LIVE=1 KOSLI_ORG=my-org KOSLI_API_TOKEN=... \
#            [KOSLI_ENVIRONMENT=e2e]                # empty = org default
#            [KOSLI_HOST=https://app.kosli.com] \
#            [COMPLIANT_IMAGE=repo@sha256:<digest>] ./hack/kind-e2e.sh
#          COMPLIANT_IMAGE must be an artifact that is compliant against the
#          policies attached to KOSLI_ENVIRONMENT (see README, "Live Kosli
#          mode"). Without it the compliant-admission case is skipped; the
#          two denial cases run either way. The compliant-tag case (webhook
#          resolves a tag to its digest via the registry) runs in mock mode
#          only, since it needs a tag whose digest is compliant.
set -euo pipefail

CLUSTER=kosli-e2e
IMAGE=kosli-webhook:e2e
NS=kosli-system
CHART=charts/kosli-admission-webhook

cleanup() {
  local rc=$?
  local keep_reason=""
  if [[ "${KEEP:-0}" == "1" ]]; then
    keep_reason="KEEP=1"
  elif [[ $rc -ne 0 && "${KEEP_ON_FAIL:-0}" == "1" ]]; then
    keep_reason="KEEP_ON_FAIL=1 and exit code $rc"
  fi
  if [[ -n "$keep_reason" ]]; then
    echo "==> $keep_reason: leaving everything in place"
    echo "    inspect:  kubectl -n $NS get all"
    echo "    logs:     kubectl -n $NS logs -l app.kubernetes.io/instance=kosli-webhook -f --prefix --tail=-1  # all replicas"
    echo "    tear down later: KEEP=0 CLUSTER_ONLY=1 ./hack/kind-e2e.sh  (or: kind delete cluster --name $CLUSTER)"
    return "$rc"
  fi
  echo "==> cleanup"
  # cluster (removes all in-cluster resources: webhook, mock, secrets, webhook config)
  kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
  # throwaway image built by this script
  docker rmi "$IMAGE" >/dev/null 2>&1 || true
  # cached images other tools may reuse: only on request
  if [[ "${CLEAN_IMAGES:-0}" == "1" ]]; then
    docker images --format '{{.Repository}}:{{.Tag}}' | grep '^kindest/node:' \
      | xargs -r docker rmi >/dev/null 2>&1 || true
  fi
  rm -f /tmp/kosli-e2e-img.tar
  return "$rc"
}
trap cleanup EXIT

# convenience: CLUSTER_ONLY=1 just runs cleanup for a previously kept cluster
if [[ "${CLUSTER_ONLY:-0}" == "1" ]]; then
  exit 0
fi

LIVE="${LIVE:-0}"
if [[ "$LIVE" == "1" ]]; then
  : "${KOSLI_ORG:?LIVE=1 requires KOSLI_ORG}"
  : "${KOSLI_API_TOKEN:?LIVE=1 requires KOSLI_API_TOKEN}"
  # optional: empty asserts against the org default instead of an environment
  KOSLI_ENVIRONMENT="${KOSLI_ENVIRONMENT:-}"
  KOSLI_HOST="${KOSLI_HOST:-https://app.kosli.com}"
  echo "==> LIVE mode: $KOSLI_HOST org=$KOSLI_ORG environment=${KOSLI_ENVIRONMENT:-<org default>}"
fi

echo "==> 1/7 kind cluster"
REUSED=0
if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  if [[ "${FRESH:-0}" == "1" ]]; then
    echo "    FRESH=1: deleting existing cluster $CLUSTER"
    kind delete cluster --name "$CLUSTER"
    kind create cluster --name "$CLUSTER" --wait 120s
  else
    REUSED=1
    echo "    reusing existing cluster $CLUSTER (set FRESH=1 to recreate)"
    kubectl config use-context "kind-$CLUSTER" >/dev/null
  fi
else
  kind create cluster --name "$CLUSTER" --wait 120s
fi

echo "==> 2/7 cert-manager"
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
kubectl -n cert-manager wait --for=condition=Available deploy --all --timeout=180s

echo "==> 3/7 build + load webhook image"
# BUILDX_NO_DEFAULT_ATTESTATIONS: provenance manifests break `kind load` when
# Docker uses the containerd image store (harmless on the classic builder).
BUILDX_NO_DEFAULT_ATTESTATIONS=1 docker build -t "$IMAGE" .
if ! kind load docker-image "$IMAGE" --name "$CLUSTER"; then
  # containerd-image-store workaround: export a single platform explicitly
  # (docker save --platform requires Docker 25+, which is exactly the
  # environment where the direct load fails)
  echo "    direct load failed; retrying with single-platform archive"
  ARCH=$(docker version -f '{{.Server.Arch}}')
  docker save --platform "linux/$ARCH" -o /tmp/kosli-e2e-img.tar "$IMAGE"
  kind load image-archive /tmp/kosli-e2e-img.tar --name "$CLUSTER"
  rm -f /tmp/kosli-e2e-img.tar
fi

echo "==> 4/7 resolve the 'compliant' test image"
if [[ "$LIVE" == "1" ]]; then
  # Caller supplies an artifact that is actually compliant in their Kosli env.
  COMPLIANT_REF="${COMPLIANT_IMAGE:-}"
  [[ -n "$COMPLIANT_REF" ]] && echo "    compliant image: $COMPLIANT_REF" \
    || echo "    COMPLIANT_IMAGE not set: compliant-admission case will be skipped"
else
  # The cluster pulls busybox@<digest> from Docker Hub itself; we only need the
  # digest, which imagetools reads from the registry without downloading blobs.
  GOOD_DIGEST=$(docker buildx imagetools inspect busybox:latest | awk '/^Digest:/{print $2}' | cut -d: -f2)
  [[ -n "$GOOD_DIGEST" ]] || { echo "could not resolve busybox digest"; exit 1; }
  COMPLIANT_REF="busybox@sha256:$GOOD_DIGEST"
  echo "    compliant fingerprint: sha256:$GOOD_DIGEST"
fi

kubectl create ns "$NS" --dry-run=client -o yaml | kubectl apply -f -
if [[ "$LIVE" == "1" ]]; then
  echo "==> 5/7 skipped (LIVE mode: using real Kosli API)"
else
echo "==> 5/7 mock Kosli assert API (in-cluster, plain HTTP)"
kubectl -n "$NS" apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: mock-kosli
data:
  server.py: |
    import http.server, json, os
    GOOD = os.environ["COMPLIANT_FP"]
    class H(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            # /api/v2/asserts/{org}/fingerprint/{sha}?...
            fp = self.path.split("?")[0].rstrip("/").split("/")[-1]
            if fp == GOOD:
                body = json.dumps({"compliant": True, "policy_evaluations": []}).encode()
                self.send_response(200)
            else:
                body = json.dumps({"message": "not found"}).encode()
                self.send_response(404)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(body)
        def log_message(self, *a): pass
    http.server.HTTPServer(("", 8080), H).serve_forever()
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mock-kosli
spec:
  replicas: 1
  selector: { matchLabels: { app: mock-kosli } }
  template:
    metadata: { labels: { app: mock-kosli } }
    spec:
      containers:
        - name: mock
          image: python:3.12-alpine
          command: ["python", "/app/server.py"]
          env: [{ name: COMPLIANT_FP, value: "$GOOD_DIGEST" }]
          volumeMounts: [{ name: app, mountPath: /app }]
      volumes:
        - name: app
          configMap: { name: mock-kosli }
---
apiVersion: v1
kind: Service
metadata:
  name: mock-kosli
spec:
  selector: { app: mock-kosli }
  ports: [{ port: 8080 }]
EOF
kubectl -n "$NS" wait --for=condition=Available deploy/mock-kosli --timeout=120s
fi

echo "==> 6/7 install the webhook chart (failurePolicy=Fail for strict testing)"
if [[ "$LIVE" == "1" ]]; then
  HOST="$KOSLI_HOST"; ORG="$KOSLI_ORG"; ENVNAME="$KOSLI_ENVIRONMENT"; TOKEN="$KOSLI_API_TOKEN"
else
  HOST="http://mock-kosli.$NS.svc:8080"; ORG=e2e; ENVNAME=e2e; TOKEN=dummy
fi
kubectl -n "$NS" create secret generic kosli-credentials \
  --from-literal=api-token="$TOKEN" --dry-run=client -o yaml | kubectl apply -f -
helm upgrade --install kosli-webhook "$CHART" -n "$NS" \
  --set image.repository="${IMAGE%%:*}" --set image.tag="${IMAGE##*:}" \
  --set image.pullPolicy=Never \
  --set kosli.host="$HOST" \
  --set-string kosli.org="$ORG" --set-string kosli.environment="$ENVNAME" \
  --set kosli.existingSecret=kosli-credentials \
  --set webhook.failurePolicy=Fail \
  --set webhook.cacheTTL=0s \
  --wait --timeout 180s
if [[ "$REUSED" == "1" ]]; then
  # same image tag + pullPolicy Never: helm sees no spec change on a reused
  # cluster, so running pods would keep the previously loaded image
  echo "    restarting deployments to pick up the freshly loaded image"
  kubectl -n "$NS" rollout restart deploy
  for d in $(kubectl -n "$NS" get deploy -o name); do
    kubectl -n "$NS" rollout status "$d" --timeout=180s
  done
fi

# helm --wait covers the Deployment only: cert-manager's cainjector fills the
# webhook's caBundle asynchronously, and until it does the API server cannot
# call the webhook — failurePolicy=Fail then rejects pods with "failed calling
# webhook" (x509) instead of a Kosli verdict. Only a fresh install races this.
echo "    waiting for the CA bundle on the webhook configuration"
for i in $(seq 1 30); do
  ca=$(kubectl get validatingwebhookconfiguration kosli-webhook-kosli-admission-webhook \
    -o jsonpath='{.webhooks[0].clientConfig.caBundle}' 2>/dev/null || true)
  [[ -n "$ca" ]] && break
  if [[ $i -eq 30 ]]; then echo "    caBundle not injected after 60s"; exit 1; fi
  sleep 2
done

# rollout status returns when the NEW pods are ready, but the OLD pods keep
# serving admission reviews during their graceful drain (the API server holds
# keep-alive connections to them). A check that fires in that window gets
# answered — and logged — by a pod that vanishes seconds later, which makes
# the decision log look empty. Wait until no webhook pod is Terminating.
echo "    waiting for old webhook pods to finish draining"
for i in $(seq 1 60); do
  if ! kubectl -n "$NS" get pods -l app.kubernetes.io/instance=kosli-webhook \
      -o jsonpath='{.items[*].metadata.deletionTimestamp}' 2>/dev/null | grep -q .; then
    break
  fi
  if [[ $i -eq 60 ]]; then echo "    old pods still terminating after 120s"; exit 1; fi
  sleep 2
done

echo "==> 7/7 assertions"
pass=0; fail=0
check() { # name, expected(0=admitted,1=denied), image
  local name=$1 expected=$2 out got; shift 2
  # leftover from an aborted run on a reused cluster would fail kubectl run
  kubectl delete pod "$name" --ignore-not-found >/dev/null 2>&1
  if out=$(kubectl run "$name" --image="$1" --restart=Never 2>&1); then got=0; else got=1; fi
  kubectl delete pod "$name" --ignore-not-found >/dev/null 2>&1
  if [[ $got == "$expected" ]]; then echo "  PASS $name"; pass=$((pass+1)); else echo "  FAIL $name (expected $expected, got $got)"; fail=$((fail+1)); fi
  # show the API server's admission response, indented (the denial message
  # from the webhook is the interesting part of this whole demo)
  echo "$out" | sed 's/^/       | /'
}

if [[ -n "${COMPLIANT_REF:-}" ]]; then
  check compliant-digest 0 "$COMPLIANT_REF"
else
  echo "  SKIP compliant-digest (set COMPLIANT_IMAGE=repo@sha256:<digest> of an artifact compliant in '${KOSLI_ENVIRONMENT:-the org default}')"
fi
if [[ "$LIVE" != "1" ]]; then
  # the webhook resolves the tag to the same digest the mock knows as compliant
  check compliant-tag    0 "busybox:latest"
fi
check unknown-digest     1 "busybox@sha256:$(printf 'a%.0s' {1..64})"
# resolves via Docker Hub to a digest the Kosli scope has never seen
check unknown-tag        1 "nginx:latest"

if [[ "${SHOW_LOGS:-0}" == "1" ]]; then
  echo
  echo "==> webhook decision log for this run (survives pod-restart/tail timing)"
  kubectl -n "$NS" logs -l app.kubernetes.io/instance=kosli-webhook --prefix --tail=-1 2>/dev/null \
    | grep -E '"msg":"(admission decision|kosli assert result|resolved tag to digest)"' \
    | sed 's/^/    /' || echo "    (no decision lines found in current pods)"
fi

echo
echo "passed=$pass failed=$fail"
[[ $fail == 0 ]]
