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
ACCESS_TOKEN=telegram_userId:access_token
```

The `SERVER_ADDR` should be a gRPC server address that the Memos is running on. It follows the [gRPC Name Resolution](https://github.com/grpc/grpc/blob/master/doc/naming.md).

`ACCESS_TOKEN` keeps the telegram bot logged in after a memogram reboot. You can get `telegram_userId` from [@userinfobot](https://t.me/userinfobot).

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

### Interaction Commands

- `/start <access_token>`: Start the bot with your Memos access token.
- Send text messages: Save the message content as a memo.
- Send files (photos, documents): Save the files as resources in a memo.
