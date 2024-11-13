# Build stage
FROM golang:1.23-alpine AS builder

# Install system dependencies
RUN apk add --no-cache make git nodejs npm

# Install pnpm globally
RUN npm install -g pnpm

# Set up frontend dependencies
WORKDIR /build
COPY ui/package.json ui/pnpm-lock.yaml ./ui/
RUN cd ui && pnpm install

# Copy frontend source and build it
COPY ui/ ./ui/
RUN cd ui && pnpm build

# Set up Go dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build the backend
COPY . .
RUN CGO_ENABLED=0 make build

# Final stage
FROM alpine:latest

RUN apk add --no-cache sqlite-libs

WORKDIR /app
COPY --from=builder /build/bin/lil.bin /app/lil
COPY config.toml /app/

CMD ["/app/lil"]
