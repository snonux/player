# syntax=docker/dockerfile:1

# ---- Builder stage ----
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Install git for go mod download if needed.
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o player ./cmd/mediaplayer

# ---- Runtime stage ----
FROM alpine:3.21

# Install ffmpeg and ffprobe.
RUN apk add --no-cache ffmpeg

# Create a non-root user (nobody already exists as 65534).
USER 65534:65534

WORKDIR /app

# Copy the binary and static assets from the builder.
COPY --from=builder --chown=65534:65534 /build/player /app/player
COPY --from=builder --chown=65534:65534 /build/web /app/web

# Expose the default HTTP port.
EXPOSE 8080

ENTRYPOINT ["/app/player"]
