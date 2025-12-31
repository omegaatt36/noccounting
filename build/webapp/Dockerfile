FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install ca-certificates for HTTPS requests
RUN apk add --no-cache ca-certificates

# Download dependencies first (for better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /webapp ./cmd/webapp

# Final stage
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /webapp /webapp

EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/webapp"]
