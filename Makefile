IMAGE ?= ghcr.io/kosli-dev/kosli-webhook
TAG   ?= dev
CHART := charts/kosli-admission-webhook

.PHONY: build test vet lint docker helm-lint helm-template helm-package all

all: vet test build helm-lint

build:
	CGO_ENABLED=0 go build -o bin/webhook ./cmd/webhook

test:
	go test ./...

vet:
	go vet ./...

docker:
	docker build -t $(IMAGE):$(TAG) .

helm-lint:
	helm lint $(CHART) --set kosli.org=ci --set kosli.environment=ci --set kosli.existingSecret=ci

helm-template:
	helm template ci $(CHART) --set kosli.org=ci --set kosli.environment=ci --set kosli.existingSecret=ci

helm-package:
	helm package $(CHART) -d dist/
