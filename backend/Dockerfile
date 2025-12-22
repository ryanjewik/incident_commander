# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies including librdkafka
RUN apk add --no-cache gcc g++ make musl-dev librdkafka-dev pkgconfig

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with CGO enabled and static linking
RUN CGO_ENABLED=1 GOOS=linux go build -tags musl -o main .

# Production stage
FROM alpine:latest

WORKDIR /root/

# Install runtime dependencies for librdkafka
RUN apk add --no-cache librdkafka

# Copy the binary from builder
COPY --from=builder /app/main .

# Copy the client.properties file
COPY client.properties .

EXPOSE 8080

CMD ["./main"]
