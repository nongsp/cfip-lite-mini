# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/cfip-lite-mini .

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 app

COPY --from=builder /out/cfip-lite-mini /usr/local/bin/cfip-lite-mini

USER app
WORKDIR /data

# Mount config.yaml here; output files are written here too.
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/cfip-lite-mini"]
