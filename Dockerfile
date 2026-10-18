# Build stage
FROM golang:1.27.2-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION
ARG COMMIT
RUN test -n "$VERSION" && test -n "$COMMIT" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w -X github.com/usememos/memogram/internal/version.Version=${VERSION} -X github.com/usememos/memogram/internal/version.Commit=${COMMIT}" \
    -o /memogram ./cmd/memogram && /memogram --version
RUN mkdir /data && chown 65532:65532 /data

# Run stage
FROM cgr.dev/chainguard/static:latest
WORKDIR /app
ENV DATA=/data/data.txt
COPY --from=builder --chown=65532:65532 /data /data
COPY --from=builder /memogram /app/memogram
USER 65532:65532
ENTRYPOINT ["/app/memogram"]
