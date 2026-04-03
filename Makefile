BINARY := kubectl-rustnet
VERSION ?= dev
IMAGE ?= ghcr.io/domcyrus/rustnet:latest
KIND_CLUSTER := rustnet-e2e
LDFLAGS := -s -w -X main.version=$(VERSION)

# Dependency checks
check-go:
	@which go > /dev/null 2>&1 || { echo "Error: go is not installed. Install: brew install go"; exit 1; }

check-kubectl:
	@which kubectl > /dev/null 2>&1 || { echo "Error: kubectl is not installed. Install: brew install kubectl"; exit 1; }

check-kind:
	@which kind > /dev/null 2>&1 || { echo "Error: kind is not installed. Install: brew install kind"; exit 1; }

check-docker:
	@which docker > /dev/null 2>&1 || { echo "Error: docker is not installed. Install: brew install --cask docker"; exit 1; }

check-golangci-lint:
	@which golangci-lint > /dev/null 2>&1 || { echo "Error: golangci-lint is not installed. Install: brew install golangci-lint"; exit 1; }

.PHONY: build test vet lint clean e2e e2e-setup e2e-teardown e2e-all install help \
        check-go check-kubectl check-kind check-docker check-golangci-lint

build: check-go ## Build the plugin binary
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/kubectl-rustnet

test: check-go ## Run unit tests
	go test ./internal/... -v

vet: check-go ## Run go vet
	go vet ./...

lint: vet check-golangci-lint ## Run linters
	golangci-lint run

e2e-setup: build check-kind check-docker check-kubectl ## Create kind cluster, load image, deploy demo workloads
	kind create cluster --name $(KIND_CLUSTER) --config e2e/kind-config.yaml --wait 60s
	docker pull $(IMAGE) || true
	kind load docker-image $(IMAGE) --name $(KIND_CLUSTER) || true
	kubectl apply -f e2e/workloads.yaml --context kind-$(KIND_CLUSTER)
	@echo "Waiting for demo workloads to start..."
	kubectl wait --for=condition=available deployment --all -n demo-traffic --timeout=120s --context kind-$(KIND_CLUSTER)

e2e: build check-kubectl ## Run e2e tests (requires e2e-setup)
	KUBECTL_RUSTNET_BIN=$(CURDIR)/$(BINARY) go test ./e2e/ -v -timeout 300s

e2e-teardown: check-kind ## Delete kind cluster
	kind delete cluster --name $(KIND_CLUSTER) || true

e2e-all: e2e-setup e2e e2e-teardown ## Setup, run e2e tests, teardown

install: build ## Install to ~/.local/bin
	mkdir -p ~/.local/bin
	cp $(BINARY) ~/.local/bin/$(BINARY)

clean: ## Remove build artifacts
	rm -f $(BINARY)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
