# Memos API snapshots

These specifications come from [usememos/dotcom/openapi](https://github.com/usememos/dotcom/tree/main/openapi).

- `latest.yaml` tracks the Memos `main` API.
- `26-10.yaml` and `26-11.yaml` represent the two most recent stable release series.
- `generate.sh` pins the dotcom commit and oapi-codegen version. Normal builds use the committed client and do not fetch schemas.

Run `go generate ./...` from the repository root to refresh the snapshots and regenerate `internal/memosapi/client.gen.go`. Only the operations Memogram and its compatibility tests use are generated.

When a new stable series ships, replace the oldest snapshot with the new series, update the pinned dotcom commit, and update the CI compatibility matrix. Run the integration test against fresh containers for all three supported versions before committing.

The latest generated REST client is used for all supported versions. Both supported series serve the API under `/api`; the 0.x series' `/api/v1` is not supported. Contract tests compare the used routes and JSON envelopes across snapshots; container tests verify their actual behavior.
