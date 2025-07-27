# golang dockerfile
FROM golang:1.23 AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

RUN apt-get update && apt-get install -y \
    build-essential \
    pkg-config \
    librdkafka-dev \
    && rm -rf /var/lib/apt/lists/*

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source from the current directory to the Working Directory inside the container
COPY . .

# Build the Go app
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags '-linkmode external -extldflags "-static"' -a -installsuffix cgo -o main ./cmd/main.go

# Path: Dockerfile
# golang dockerfile
FROM gcr.io/distroless/static-debian10

EXPOSE 3000
# Set the Current Working Directory inside the container
WORKDIR /app

# Copy the Pre-built binary file from the previous stage
COPY --from=builder --chown=nonroot:nonroot /app/main .

ARG VERSION

ENV VERSION=$VERSION

USER nonroot

CMD ["./main", "serve"]

# Expose port 8080 to the outside world