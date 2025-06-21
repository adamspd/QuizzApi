# Start from minimal base
FROM alpine:latest

# Install runtime dependencies only
RUN apk --no-cache add ca-certificates sqlite wget

# Create app directory and required subdirs
WORKDIR /app
RUN mkdir -p /app/data /app/logs

# Copy your pre-built binary
COPY QuizzApi .

# Make sure it's executable
RUN chmod +x ./QuizzApi

# Expose port
EXPOSE 8043

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8043/health || exit 1

# Run it
CMD ["./QuizzApi"]
