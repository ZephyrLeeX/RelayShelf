.DEFAULT_GOAL := build

CONTAINER_RUNTIME ?= podman

# Release metadata injected into the container build. CI and release tooling
# pass these explicitly; local defaults fall back to git state, which may be
# unavailable when building from an exported tree.
RELEASE_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo development)
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: generate lint test build e2e integration container

generate:
	./scripts/generate.sh

lint:
	@test -z "$$(gofmt -l $$(find cmd internal sql/generated -name '*.go' -type f))" || (echo 'Go files are not formatted; run gofmt.' >&2; exit 1)
	go vet ./...
	golangci-lint run
	cd web && pnpm lint && pnpm typecheck

test:
	go test ./...
	cd web && pnpm test --run
	./scripts/test-integration.sh

build:
	cd web && pnpm build
	mkdir -p internal/webui/dist bin
	find internal/webui/dist -mindepth 1 -delete
	cp -R web/dist/. internal/webui/dist/
	go build -o bin/relayshelf ./cmd/relayshelf

e2e:
	cd web && pnpm e2e

integration:
	./scripts/test-integration.sh

container:
	$(CONTAINER_RUNTIME) build \
		--build-arg VERSION="$(RELEASE_VERSION)" \
		--build-arg GIT_COMMIT="$(GIT_COMMIT)" \
		--build-arg BUILD_TIME="$(BUILD_TIME)" \
		-t relayshelf:ci .
