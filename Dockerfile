FROM --platform=$BUILDPLATFORM cgr.dev/chainguard/go:latest AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /memogram ./bin/memogram

FROM cgr.dev/chainguard/static:latest

WORKDIR /app

ENV SERVER_ADDR=dns:localhost:5230
ENV DATA=/app/data/data.txt

COPY --from=builder /memogram /app/memogram

USER nonroot:nonroot

CMD ["/app/memogram"]
