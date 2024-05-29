# Build stage
FROM golang:1.22.2-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o memogram ./bin/memogram
RUN chmod +x memogram

# Run stage
FROM alpine:latest
WORKDIR /app
ENV SERVER_ADDR=dns:localhost:5230
ENV BOT_TOKEN=your_telegram_bot_token
COPY .env.example .env
COPY --from=builder /app/memogram .
CMD ["./memogram"]
