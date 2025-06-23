# Memogram

**Memogram** is an easy to use integration service for syncing messages and images from a Telegram bot into your Memos.

## Prerequisites

- Memos service
- Telegram Bot

## Installation

Download the binary files for your operating system from the [Releases](https://github.com/usememos/telegram-integration/releases) page.

## Configuration

Create a `.env` file in the project's root directory and add the following configuration:

```env
SERVER_ADDR=dns:localhost:5230
BOT_TOKEN=your_telegram_bot_token
BOT_PROXY_ADDR=https://api.your_proxy_addr.com
ALLOWED_USERNAMES=user1,user2,user3
```

### Configuration Options

- `SERVER_ADDR`: The gRPC server address where Memos is running
- `BOT_TOKEN`: Your Telegram bot token
- `BOT_PROXY_ADDR`: Optional proxy address for Telegram API (leave empty if not needed)
- `ALLOWED_USERNAMES`: Optional comma-separated list of allowed usernames (without @ symbol)

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
- The bot will only respond to users who have a username set in their Telegram account
- Users not in the allowed list will receive an error message: "you are not authorized to use this bot"

The `SERVER_ADDR` should be a gRPC server address that the Memos is running on. It follows the [gRPC Name Resolution](https://github.com/grpc/grpc/blob/master/doc/naming.md).

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
    -e SERVER_ADDR=dns:localhost:5230 \
    -e BOT_TOKEN=your_telegram_bot_token \
    memogram
    ```

3.  The Memogram service should now be running inside the Docker container. You can interact with it via your Telegram bot.

#### Starting with Docker Compose

Or you can start the service with Docker Compose. This can be combined with the `memos` itself in the same compose file:

1.  Create a folder where the service will be located.
2.  Clone this repository in a subfolder `git clone https://github.com/usememos/telegram-integration memogram`
3.  Create `.env` file
    ```sh
    SERVER_ADDR=dns:yourMemosUrl.com:5230
    BOT_TOKEN=your_telegram_bot_token
    ```
4.  Create Docker Compose `docker-compose.yml` file:
    ```yaml
    services:
      memogram:
        env_file: .env
        build: memogram
        container_name: memogram
    ```
5.  Run the bot via `docker compose up -d`
6.  The Memogram service should now be running inside the Docker container. You can interact with it via your Telegram bot.

### Interaction Commands

- `/start <access_token>`: Start the bot with your Memos access token.
- Send text messages: Save the message content as a memo.
- Send files (photos, documents): Save the files as resources in a memo.
- `/search <words>`: Search for the memos.
