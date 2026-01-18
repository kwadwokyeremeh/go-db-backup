FROM golang:1.21-alpine AS builder

# Install database clients
RUN apk add --no-cache \
    mysql-client \
    postgresql-client \
    redis

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o db-backup main.go

# Final stage
FROM alpine:latest

# Install database clients and utilities
RUN apk --no-cache add \
    ca-certificates \
    mysql-client \
    postgresql-client \
    redis \
    bash \
    && addgroup -g 65532 nonroot \
    && adduser -D -u 65532 -G nonroot nonroot

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/db-backup .

# Create backups directory
RUN mkdir -p /backups && chown -R nonroot:nonroot /backups

USER nonroot

# Expose nothing (this is a backup utility, not a web server)
EXPOSE 8080

# Run the application
ENTRYPOINT ["./db-backup"]

# Default command (empty to allow environment variables to take precedence)
CMD []