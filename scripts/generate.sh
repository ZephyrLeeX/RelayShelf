#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml > internal/httpapi/openapi.gen.go
pnpm --dir web exec openapi --input ../api/openapi.yaml --output src/api/generated --client fetch
go tool sqlc generate -f sql/sqlc.yaml
