# Build stage
FROM golang:1.27.2-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -o /memogram ./bin/memogram
RUN mkdir /data && chown 65532:65532 /data

# Run stage
FROM cgr.dev/chainguard/static:latest
WORKDIR /app
ENV DATA=/data/data.txt
COPY --from=builder --chown=65532:65532 /data /data
COPY --from=builder /memogram /app/memogram
USER 65532:65532
ENTRYPOINT ["/app/memogram"]
