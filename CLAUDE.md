# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is an image synchronization service for anime data, serving as a POC since pulsar mariadb sync does not work. The service handles image processing and storage for anime records, supporting both Pulsar and Kafka as message brokers.

## Architecture

The application follows a standard Go project structure:

- **cmd/**: Application entry point (`main.go`)
- **internal/**: Private application code
  - **commands/**: CLI commands using Cobra framework
  - **eventing/**: Event handlers for Pulsar and Kafka
  - **services/**: Business logic services
    - **image_processor/**: Core image processing logic
    - **image_processor_kafka/**: Kafka-specific image processing
    - **storage/**: Storage implementations (MinIO)
    - **processor/**: Generic message processing
  - **db/**: Database connection and utilities
  - **logger/**: Logging utilities
- **config/**: Configuration management
- **db/migrations/**: Database migration files

## Key Components

### Message Processing
The system supports two messaging backends:
- **Pulsar**: `serve-image-sync` command uses Apache Pulsar
- **Kafka**: `serve-image-sync-kafka` command uses Confluent Kafka

### Storage
- Uses MinIO for object storage
- MySQL database for metadata (via GORM)

Objects are keyed by the record's **id**, never its name: `<anime id>` for
posters, `banners/<anime id>`, `characters/<character id>`, `staff/<staff id>`.
Producers send both `id` and `name` on the image message; `name` is only a
fallback for messages published before ids were added. `internal/services/imagepath`
owns that rule so the pulsar and kafka processors cannot drift apart, and
`backfill-image-ids` re-keys objects still stored under the old name slugs.

### Configuration
Configuration is handled via `config/config.go` with environment variable support and JSON config files.

## Common Commands

### Development
```bash
# Run with Docker Compose (MySQL + Redis)
docker-compose up

# Build the application
go build -o main ./cmd/main.go

# Run Pulsar-based image sync
./main serve-image-sync

# Run Kafka-based image sync
./main serve-image-sync-kafka

# Re-key existing bucket objects from names to ids (one-off, idempotent)
./main backfill-image-ids --dry-run
./main backfill-image-ids
./main backfill-image-ids --type anime,character --workers 16

# Database migrations
./main migrate up
./main migrate down

# Create new migration
make migrate-create name=migration_name
```

### Docker
```bash
# Build Docker image
docker build -t image-sync .

# Run container
docker run -p 3000:3000 image-sync serve
```

## Dependencies

Key external dependencies:
- **Apache Pulsar**: Message streaming
- **Confluent Kafka**: Alternative message streaming
- **MinIO**: Object storage
- **GORM**: ORM for MySQL
- **Cobra**: CLI framework
- **Zap**: Structured logging

## Environment Variables

The application uses environment variables for configuration:
- `DBHOST`, `DBNAME`, `DBUSERNAME`, `DBPASSWORD`, `DBPORT`: Database connection
- `PULSARURL`, `PULSARTOPIC`, `PULSARSUBSCRIPTIONNAME`: Pulsar configuration
- `KAFKA_BOOTSTRAP_SERVERS`, `KAFKA_TOPIC`, `KAFKA_CONSUMER_GROUP_NAME`: Kafka configuration
- `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY_ID`, `MINIO_SECRET_ACCESS_KEY`: MinIO configuration

## Database

Uses MySQL with GORM. Migrations are managed via golang-migrate in the `db/migrations/` directory.