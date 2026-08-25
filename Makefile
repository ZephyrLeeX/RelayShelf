.DEFAULT_GOAL := build

.PHONY: generate lint test build e2e integration container

generate:
	./scripts/generate.sh

lint:
	@test -z "$$(gofmt -l $$(find cmd internal sql/generated -name '*.go' -type f))" || (echo 'Go files are not formatted; run gofmt.' >&2; exit 1)
	go vet ./...
	cd web && pnpm lint && pnpm typecheck

test:
	go test ./...
	cd web && pnpm test --run
	./scripts/test-integration.sh

build:
	mkdir -p bin
	go build -o bin/share-system ./cmd/share-system
	cd web && pnpm build

e2e:
	cd web && pnpm e2e

integration:
	./scripts/test-integration.sh

container:
	docker build -t relay-shelf:ci .
