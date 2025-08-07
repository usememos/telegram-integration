# Build stage
FROM cgr.dev/chainguard/go:latest AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o memogram ./bin/memogram
RUN chmod +x memogram

# Run stage
FROM cgr.dev/chainguard/static:latest-glibc

# Create a non-root user and group
# Chainguard images often run as uid 65532 (nonroot)
USER 65532:65532

WORKDIR /app

ENV SERVER_ADDR=dns:localhost:5230
ENV BOT_TOKEN=your_telegram_bot_token

# Copy files with proper ownership
COPY --from=builder --chown=65532:65532 /app/memogram .
COPY --chown=65532:65532 .env.example .env

CMD ["./memogram"]
