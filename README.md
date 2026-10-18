# Memogram

**Memogram** is an easy to use integration service for syncing messages and images from a Telegram bot into your Memos.

## Prerequisites

- Memos service: latest (`main`/`canary`) or one of the two most recent stable series, currently **0.30.x and 0.31.x**
- Telegram Bot

Older Memos versions are outside the supported range. CI checks the latest image and the two stable releases with real Memos containers.

## Installation

Download the binary files for your operating system from the [Releases](https://github.com/usememos/telegram-integration/releases) page.

## Configuration

Create a `.env` file in the project's root directory and add the following configuration:

```env
SERVER_ADDR=http://localhost:5230
BOT_TOKEN=your_telegram_bot_token
BOT_PROXY_ADDR=https://api.your_proxy_addr.com
ALLOWED_USERNAMES=user1,user2,user3
DATA=data.txt
```

### Configuration Options

- `SERVER_ADDR`: The full HTTP or HTTPS base URL of your Memos instance, such as `https://memos.example.com`. Legacy `dns:localhost:5230` and bare host addresses are still accepted and normalized to HTTP.
- `BOT_TOKEN`: Your Telegram bot token
- `BOT_PROXY_ADDR`: Optional alternative Telegram API server URL (leave empty if not needed). This is not an HTTP CONNECT proxy.
- `ALLOWED_USERNAMES`: Optional comma-separated list of allowed usernames (without @ symbol)
- `DATA`: Token binding file, defaulting to `data.txt` for binaries and `/data/data.txt` inside the container. Its parent directory must be writable. Persist this file across upgrades and keep it private: it contains Memos access tokens. It holds one JSON object per user. Files written by versions before Spaces support are read as before and converted on the next change, so downgrading after that point requires every user to send `/start` again.

### Username Restrictions

The `ALLOWED_USERNAMES` environment variable allows you to restrict bot usage to specific Telegram users. When set, only users with usernames in this list will be able to interact with the bot.

#### Examples

1. Allow specific users:

   ```env
   ALLOWED_USERNAMES=alex,john,emily
   ```

2. Allow all users (leave empty or remove the variable):

   ```env
   ALLOWED_USERNAMES=
   ```

#### Important Notes

- Usernames must not include the @ symbol
- When the allowlist is set, users must have a matching Telegram username. With no allowlist, users without a username can also use the bot.
- Matching is case-insensitive and trims whitespace
- Users outside the allowed list receive an error reply, including when they press a button on an earlier message.

Memogram uses Memos' HTTP REST API. At startup it discovers `/api` (latest) or `/api/v1` (0.30/0.31) through the read-only instance profile endpoint.

## Usage

### Starting the Service

#### Starting with binary

1. Download and extract the released binary file;
2. Create a `.env` file in the same directory as the binary file;
3. Run the executable in the terminal:

   ```sh
   ./memogram
   ```

4. Once the bot is running, you can interact with it via your Telegram bot.

#### Starting with Docker

Or you can start the service with Docker:

1. Build the Docker image: `sh scripts/build.sh docker -t memogram`. The script passes the version and commit from Git to the Docker build.
2.  Run the Docker container with the required environment variables:

    ```sh
    docker run -d --name memogram \
    --restart unless-stopped \
    --mount type=volume,source=memogram-data,target=/data \
    -e SERVER_ADDR=https://memos.example.com \
    -e BOT_TOKEN=your_telegram_bot_token \
    memogram
    ```

3. The container runs as UID/GID `65532`. The named volume retains token bindings when the container is recreated. For a bind mount, give this user write access to the mounted directory.

Inside Docker, `localhost` refers to the Memogram container. Use your Memos service name (`http://memos:5230`) when both services share a Docker network. To connect to Memos on the host, use `http://host.docker.internal:5230` and add `--add-host host.docker.internal:host-gateway` to the run command.

#### Starting with Docker Compose

The repository includes `docker-compose.yml` with a persistent data volume:

```sh
git clone https://github.com/usememos/telegram-integration memogram
cd memogram
cp .env.example .env
# Edit .env: set BOT_TOKEN and a SERVER_ADDR reachable from the container.
sh scripts/build.sh compose up -d --build
```

Compose supplies `DATA=/data/data.txt`, retains bindings in `memogram-data`, and configures `host.docker.internal` for connecting to Memos on the host. Use `SERVER_ADDR=http://host.docker.internal:5230` for that setup, or your instance's HTTPS URL.

Update with `git pull --ff-only` followed by `sh scripts/build.sh compose up -d --build`. `docker compose down` preserves bindings; `docker compose down -v` removes them.

### Interaction Commands

- `/start <access_token>`: Link the bot using a Personal Access Token created in your Memos account settings.
- Send text messages: Save the message content as a memo.
- Send photos, documents, voice messages, or videos: Save the files as attachments in a memo.
- `/search <words>`: Search for the memos.
- `/space`: Choose a default space for new memos, or "No space" to keep them private. Requires Memos 0.31 or later.

Each saved memo gets buttons to make it public or private, to pin it, and, on Memos 0.31 or later, to move it into one of your spaces. A memo placed in a space gets `SPACE` visibility, so that the space's members can see it. "No space" takes it out again and makes a `SPACE` memo private.

If your default space is deleted or you leave it, the next memo is saved without it, the bot tells you so, and the default is cleared.

If an attachment cannot be downloaded or uploaded, the bot replies that the memo was saved but the attachment failed, with a link to that memo. Downloads are limited to 20 MiB and HTTP requests have a 60-second timeout.

## Development

Requires Go 1.27.2 or newer. Linting uses [golangci-lint](https://golangci-lint.run) 2.15.0 with the settings in `.golangci.yml`; build it with that Go version or newer, for example `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0`.

```sh
go test -race ./...
go vet ./...
golangci-lint run
sh scripts/build.sh
./build/memogram --version
```

Code layout:

- `cmd/memogram`: the entrypoint. It loads the configuration and wires the store, the Memos client and the bot together.
- `internal/bot`: the Telegram handlers for memo capture, attachments, albums, commands and memo buttons.
- `internal/memos`: the Memos client and API version discovery, built on the generated `internal/memosapi`.
- `internal/config`: environment configuration.
- `internal/store`: the file that binds Telegram users to access tokens.
- `internal/version`: build metadata.

The REST client is generated from the versioned OpenAPI specifications in [usememos/dotcom](https://github.com/usememos/dotcom/tree/main/openapi). See [openapi/README.md](openapi/README.md) for generation and supported-version updates.

To run the business compatibility scenarios, start a **fresh, disposable** Memos container and set `MEMOS_TEST_URL`:

```sh
docker run --rm -d --name memogram-memos-test \
  -e MEMOS_MODE=prod -p 127.0.0.1:5231:5230 neosmemo/memos:0.31.0
MEMOS_TEST_URL=http://127.0.0.1:5231 go test -race -run '^TestMemosIntegration$' -count=1 -timeout=3m -v ./internal/bot
docker rm -f memogram-memos-test
```

CI runs the same scenarios against `canary` (latest development), `0.30.0`, and `0.31.0`, with the race detector enabled. Ordinary `go test` skips this integration test unless `MEMOS_TEST_URL` is set. The test creates an administrator for setup, then uses two ordinary users and their Personal Access Tokens for business operations. It refuses to run against an instance that already has users.

The scenarios cover text saving and search; documents and images with or without captions; images sent as documents; voice and video uploads; sequential and concurrent albums; partial attachment failures and recovery; visibility and pin buttons; user isolation; permission denial; invalid or revoked tokens; and, on 0.31 and later, moving memos into and out of a space, the default space, and a default space that was deleted. On 0.30 they check that spaces are reported as unsupported. Uploaded files are downloaded from Memos and compared byte for byte. Images use a generated PNG; voice and video use small transport fixtures, without playback validation. Telegram responses are mocked; Memos API calls and file storage use the real server. An HTTP proxy injects the attachment service failure for the recovery scenario.

Verify container startup and binding persistence separately:

```sh
sh scripts/build.sh docker -t memogram
python3 scripts/test-container.py memogram
```

## Versioning and releases

The first CalVer release is **26.10.0**, following the historical `v0.5.0` release. Memogram releases are numbered independently of the Memos versions they support.

- Stable tags: `YY.MM.N`, such as `26.10.0` and `26.10.1`. Each month's sequence starts at `.0`.
- Release candidates: `YY.MM.N-rc.K`, such as `26.10.0-rc.1`. Candidate numbers start at 1.
- Development builds: `YY.MM.D-dev.<sha7>`, derived from HEAD's committer date in UTC and commit SHA, such as `26.10.15-dev.abcdef0`.

New release tags have no `v` prefix, and months are always two digits. Bare month names such as `26.10` are not release tags. Historical `v0.x` releases remain available.

`--version` prints the version and commit without loading configuration or contacting Telegram. Startup logs include the same metadata. Release builds inject the exact tag; ordinary Go package builds derive development metadata from Git. For `go run`, use `go run -buildvcs=true ./cmd/memogram --version`.

The build script supplies metadata for binary, Docker, and Compose builds. Docker's context excludes `.git`, so direct Docker builds must provide `--build-arg VERSION=... --build-arg COMMIT=...`. To build the first release locally without creating a Git tag or publishing:

```sh
MEMOGRAM_VERSION=26.10.0 sh scripts/build.sh
MEMOGRAM_VERSION=26.10.0 sh scripts/build.sh docker -t memogram:26.10.0
./build/memogram --version
```

The tag-triggered workflow validates the release format, tests the code, and uses GoReleaser 2.18.3 to build release archives. GitHub releases remain drafts, with candidates marked as prereleases. Changelogs compare against the previous stable ancestor, falling back to the legacy `v0.x` history for the first CalVer release. API support remains latest plus the two most recent stable Memos series; changing Memogram's release format does not change that matrix.

CI also runs `python3 scripts/test-release.py` with GoReleaser 2.18.3. It packages `26.10.0`, `26.10.0-rc.1`, and `26.01.0` in a temporary clone, checks the native archived binary's version, and skips publishing and announcements. The test creates tags only in the disposable clone.
