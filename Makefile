IMAGE ?= ghcr.io/kosli-dev/kosli-webhook
TAG   ?= dev
CHART := charts/kosli-admission-webhook

# defaults for e2e-live (override: make e2e-live KOSLI_ORG=my-org ...)
KOSLI_ORG       ?= kosli-public
COMPLIANT_IMAGE ?= ghcr.io/kosli-dev/cli:v2.33.1

.PHONY: help build test vet docker helm-lint helm-template helm-package all e2e e2e-mock e2e-live e2e-clean
.DEFAULT_GOAL := help

all: vet test build helm-lint ## vet + test + build + helm-lint

help: ## show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## build bin/webhook (static, CGO off)
	CGO_ENABLED=0 go build -o bin/webhook ./cmd/webhook

test: ## run unit tests
	go test ./...

vet: ## run go vet
	go vet ./...

docker: ## build the container image (override IMAGE=... TAG=...)
	docker build -t $(IMAGE):$(TAG) .

helm-lint: ## lint the chart with CI dummy values
	helm lint $(CHART) --set kosli.org=ci --set kosli.environment=ci --set kosli.existingSecret=ci

helm-template: ## render the chart with CI dummy values
	helm template ci $(CHART) --set kosli.org=ci --set kosli.environment=ci --set kosli.existingSecret=ci

helm-package: ## package the chart into dist/
	helm package $(CHART) -d dist/

# flags pass through the environment, e.g.:
#   LIVE=1 KOSLI_ORG=... KOSLI_API_TOKEN=... make e2e
#   KEEP_ON_FAIL=1 SHOW_LOGS=1 make e2e      # debugging loop
# (see the hack/kind-e2e.sh header for all flags)
e2e: ## kind end-to-end test (env flags: LIVE, KEEP, KEEP_ON_FAIL, FRESH, SHOW_LOGS)
	./hack/kind-e2e.sh

# forces mock mode even if LIVE=1 is exported in the shell; no credentials
# needed. KEEP/SHOW_LOGS/FRESH etc. pass through, e.g.: KEEP=1 make e2e-mock
e2e-mock: ## e2e against the in-cluster mock Kosli API (no credentials needed)
	LIVE=0 ./hack/kind-e2e.sh

# needs KOSLI_API_TOKEN in the environment; KEEP/SHOW_LOGS/KOSLI_ENVIRONMENT
# etc. pass through, e.g.: KEEP=1 make e2e-live
e2e-live: ## e2e against the real Kosli API (defaults: KOSLI_ORG=kosli-public, Kosli CLI image)
	@test -n "$(KOSLI_API_TOKEN)" || { echo "KOSLI_API_TOKEN is required (export it or pass on the command line)"; exit 1; }
	LIVE=1 KOSLI_ORG=$(KOSLI_ORG) COMPLIANT_IMAGE=$(COMPLIANT_IMAGE) ./hack/kind-e2e.sh

e2e-clean: ## tear down a cluster kept by KEEP=1 / KEEP_ON_FAIL=1
	KEEP=0 CLUSTER_ONLY=1 ./hack/kind-e2e.sh
