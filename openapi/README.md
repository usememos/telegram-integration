# Memos API snapshots

These specifications come from [usememos/dotcom/openapi](https://github.com/usememos/dotcom/tree/main/openapi).

- `latest.yaml` tracks the Memos `main` API.
- `0-30.yaml` and `0-31.yaml` represent the two most recent stable release series.
- `generate.sh` pins the dotcom commit and oapi-codegen version. Normal builds use the committed client and do not fetch schemas.

Run `go generate ./...` from the repository root to refresh the snapshots and regenerate `internal/memosapi/client.gen.go`. Only the operations Memogram and its compatibility tests use are generated.

When a new stable series ships, replace the oldest snapshot with the new series, update the pinned dotcom commit, and update the CI compatibility matrix. Run the integration test against fresh containers for all three supported versions before committing.

The latest generated REST client is used for all supported versions. Startup probes the read-only instance profile endpoint to select `/api` (latest) or `/api/v1` (0.30 and 0.31). Mutating requests are never replayed to discover a version. Contract tests compare the used routes and JSON envelopes across snapshots; container tests verify their actual behavior.
