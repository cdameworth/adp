# Build stage
FROM cgr.dev/chainguard/go:latest AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the server binary (use TARGETARCH for multi-platform support)
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build \
    -ldflags="-w -s -X main.version=$(cat VERSION 2>/dev/null || echo 'dev')" \
    -o /adp-server ./cmd/adp-server

# Build the CLI binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build \
    -ldflags="-w -s" \
    -o /adp-cli ./cmd/adp-cli

# Build the MCP server binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build \
    -ldflags="-w -s" \
    -o /adp-mcp ./cmd/adp-mcp

# Production stage - using Chainguard static base
FROM cgr.dev/chainguard/static:latest

# Labels
LABEL org.opencontainers.image.title="Agent Developer Portal"
LABEL org.opencontainers.image.description="Agent-agnostic infrastructure for AI governance"
LABEL org.opencontainers.image.vendor="ADP"

# Copy binaries from builder
COPY --from=builder /adp-server /usr/local/bin/adp-server
COPY --from=builder /adp-cli /usr/local/bin/adp-cli
COPY --from=builder /adp-mcp /usr/local/bin/adp-mcp

# Copy default policies
COPY --from=builder /app/policies /etc/adp/policies

# Copy migrations
COPY --from=builder /app/migrations /etc/adp/migrations

# Use non-root user (Chainguard images run as non-root by default)
USER nonroot:nonroot

# Default port
EXPOSE 8080 9090

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/adp-cli", "health", "--quiet"]

# Default entrypoint
ENTRYPOINT ["/usr/local/bin/adp-server"]
