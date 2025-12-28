FROM golang:1.24-bookworm AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY *.go ./

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o picture-metadata .

# Final stage
FROM debian:bookworm-slim

# Install exiftool
RUN apt-get update && \
    apt-get install -y libimage-exiftool-perl && \
    rm -rf /var/lib/apt/lists/*

# Copy the binary from builder
COPY --from=builder /build/picture-metadata /usr/local/bin/

# Create working directory
WORKDIR /data

# Set entrypoint
ENTRYPOINT ["picture-metadata"]
CMD ["--help"]
