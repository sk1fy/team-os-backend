#!/bin/sh
# Versions match the reviewed staging source package; no service is started.
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
docker run --rm -v "$PWD:/src" -w /src sqlc/sqlc:1.27.0 generate -f services/company/sqlc.yaml
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.2 \
  -generate types,chi-server,spec -package api \
  -o services/gateway/internal/api/teamos.gen.go contracts/openapi/teamos.yaml
