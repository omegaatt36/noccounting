FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install ca-certificates and curl for HTTPS requests
RUN apk add --no-cache ca-certificates curl libstdc++ libgcc

# Install templ for component generation
RUN go install github.com/a-h/templ/cmd/templ@latest

# Install Tailwind CSS v4 standalone CLI (musl for Alpine)
RUN curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64-musl \
    && chmod +x tailwindcss-linux-x64-musl \
    && mv tailwindcss-linux-x64-musl /usr/local/bin/tailwindcss

# Download dependencies first (for better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Generate templ components
RUN templ generate

# Build CSS
RUN tailwindcss -i ./internal/app/webapp/static/input.css -o ./internal/app/webapp/static/output.css --minify

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /webapp ./cmd/webapp

# Final stage
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /webapp /webapp

EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/webapp"]
