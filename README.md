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
- `DATA`: Token binding file, defaulting to `data.txt` for binaries and `/data/data.txt` inside the container. Its parent directory must be writable. Persist this file across upgrades and keep it private: it contains Memos access tokens.

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
- Users outside the allowed list receive an error reply.

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

1.  Build the Docker image: `docker build -t memogram .`
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
docker compose up -d --build
```

Compose supplies `DATA=/data/data.txt`, retains bindings in `memogram-data`, and configures `host.docker.internal` for connecting to Memos on the host. Use `SERVER_ADDR=http://host.docker.internal:5230` for that setup, or your instance's HTTPS URL.

Update with `git pull --ff-only` followed by `docker compose up -d --build`. `docker compose down` preserves bindings; `docker compose down -v` removes them.

### Interaction Commands

- `/start <access_token>`: Link the bot using a Personal Access Token created in your Memos account settings.
- Send text messages: Save the message content as a memo.
- Send photos, documents, voice messages, or videos: Save the files as attachments in a memo.
- `/search <words>`: Search for the memos.

If an attachment cannot be downloaded or uploaded, the bot replies that the memo was saved but the attachment failed, with a link to that memo. Downloads are limited to 20 MiB and HTTP requests have a 60-second timeout.

## Development

Requires Go 1.27.2 or newer.

```sh
go test -race ./...
go vet ./...
go build -o build/memogram ./bin/memogram
```

The REST client is generated from the versioned OpenAPI specifications in [usememos/dotcom](https://github.com/usememos/dotcom/tree/main/openapi). See [openapi/README.md](openapi/README.md) for generation and supported-version updates.

To run the full capture/search/attachment/visibility compatibility test, start a **fresh, disposable** Memos container and set `MEMOS_TEST_URL`:

```sh
docker run --rm -d --name memogram-memos-test \
  -e MEMOS_MODE=prod -p 127.0.0.1:5231:5230 neosmemo/memos:0.31.0
MEMOS_TEST_URL=http://127.0.0.1:5231 go test -run TestMemosIntegration -count=1 -v .
docker rm -f memogram-memos-test
```

CI runs this test against `canary`, `0.30.0`, and `0.31.0`. The test creates an administrator and a Personal Access Token, and refuses to run against an instance that already has users. Telegram responses are mocked; Memos API calls use the real server.

Verify container startup and binding persistence separately:

```sh
docker build -t memogram .
python3 scripts/test-container.py memogram
```
