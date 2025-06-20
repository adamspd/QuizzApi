# Dockerfile
FROM golang:1.23-alpine AS builder

# Install dependencies
RUN apk add --no-cache git gcc musl-dev sqlite-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY *.go ./

# Get any missing dependencies
RUN go mod tidy

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

# Install dependencies
RUN apk --no-cache add ca-certificates sqlite wget

# Create app directory
WORKDIR /app

# Create data directory for SQLite
RUN mkdir -p /app/data /app/logs

# Copy binary from builder
COPY --from=builder /app/main .

# Make binary executable
RUN chmod +x ./main

# Expose port
EXPOSE 8043

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8043/health || exit 1

# Run the application
CMD ["./main"]