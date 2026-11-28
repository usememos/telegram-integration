#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

# Fixed dotcom snapshot: latest follows Memos main; stable schemas follow release tags.
DOTCOM_REF=073a0692b71776640a648a550fbcf36996780332
for version in latest 26-10 26-11; do
  curl --fail --silent --show-error --location \
    "https://raw.githubusercontent.com/usememos/dotcom/$DOTCOM_REF/openapi/$version.yaml" \
    --output "openapi/$version.yaml"
done

mkdir -p internal/memosapi
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.9.0 \
  --config openapi/oapi-codegen.yaml openapi/latest.yaml
