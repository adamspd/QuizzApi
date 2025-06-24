# Build stage
FROM golang:1.23-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-linkmode external -extldflags "-static"' -o QuizzApi .

# Runtime stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates sqlite wget
WORKDIR /app
RUN mkdir -p /app/data /app/logs
COPY --from=builder /app/QuizzApi .
COPY .env ./.env
RUN chmod +x ./QuizzApi
EXPOSE 8043
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8043/health || exit 1
CMD ["./QuizzApi"]