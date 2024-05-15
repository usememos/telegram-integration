# Memogram

**Memogram** is a Go-based Telegram bot designed to store Telegram messages and files into [Memos](https://usememos.com/). It's easy to use and supports saving text, images, and files.

## Prerequisites

- Memos service
- Telegram Bot

## Installation

Download the binary files for your operating system from the [Releases](https://github.com/usememos/telegram-integration/releases) page.

## Configuration

Create a `.env` file in the project's root directory and add the following configuration:

```env
SERVER_ADDR=your_memos_server_address (e.g., https://demo.usememos.com)
BOT_TOKEN=your_telegram_bot_token
```

## Usage

### Starting the Service

1. Download and extract the released binary file;
2. Create a `.env` file in the same directory as the binary file;
3. Run the executable in the terminal:

   ```sh
   ./memogram
   ```

4. Once the bot is running, you can interact with it via your Telegram bot.

### Interaction Commands

- `/start <access_token>`: Start the bot with your Memos access token.
- Send text messages: Save the message content as a memo.
- Send files (photos, documents): Save the files as resources in a memo.
