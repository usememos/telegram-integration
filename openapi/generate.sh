#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

# Fixed dotcom snapshot: latest follows Memos main; stable schemas follow release tags.
DOTCOM_REF=db8e8a451b9f329c70ca7af82a0c8a9948f5ee51
for version in latest 0-30 0-31; do
  curl --fail --silent --show-error --location \
    "https://raw.githubusercontent.com/usememos/dotcom/$DOTCOM_REF/openapi/$version.yaml" \
    --output "openapi/$version.yaml"
done

mkdir -p internal/memosapi
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 \
  --config openapi/oapi-codegen.yaml openapi/latest.yaml
