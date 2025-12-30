FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache ca-certificates curl libstdc++ libgcc

RUN go install github.com/a-h/templ/cmd/templ@latest \
    && go install github.com/microsoft/typescript-go/cmd/tsgo@latest

ARG TARGETARCH
RUN TAILWIND_ARCH=$([ "$TARGETARCH" = "arm64" ] && echo "arm64" || echo "x64") \
    && curl -sLO "https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-${TAILWIND_ARCH}-musl" \
    && chmod +x "tailwindcss-linux-${TAILWIND_ARCH}-musl" \
    && mv "tailwindcss-linux-${TAILWIND_ARCH}-musl" /usr/local/bin/tailwindcss

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN templ generate

RUN tsgo -p web/ts/tsconfig.json

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
