# Go Database Backup System

A high-frequency database backup system written in Go, supporting MySQL/MariaDB, PostgreSQL, and Redis with S3-compatible storage (including HETZNER Object Storage).

## Features

- High-frequency database backups (configurable interval)
- Support for MySQL, MariaDB, PostgreSQL, and Redis
- Compression with gzip
- S3-compatible storage support (AWS, HETZNER, etc.)
- Automatic cleanup of old backups
- Optimized performance with nice/ionice
- Configurable retention policy
- Environment variable support for configuration

## Prerequisites

- Go 1.21+
- Database client tools:
  - `mysqldump` or `mariadb-dump` for MySQL/MariaDB
  - `pg_dump` for PostgreSQL
  - `redis-cli` for Redis
- AWS credentials for S3 storage (if using S3)

## Installation

1. Clone or copy the project files
2. Install dependencies:

```bash
go mod tidy
```

## Usage

### Basic Database Backup

```bash
go run main.go \
  -connection=mariadb \
  -db-host=localhost \
  -db-port=3306 \
  -db-name=your_database \
  -db-user=your_username \
  -db-password=your_password \
  -path=./backups \
  -interval=3600 \
  -max-files=10 \
  -gzip=true
```

### Redis Backup

```bash
go run main.go \
  -connection=redis \
  -db-host=localhost \
  -db-port=6379 \
  -db-password=your_redis_password \
  -path=./backups \
  -interval=3600 \
  -max-files=10 \
  -gzip=true
```

### With S3 Storage (AWS)

```bash
export AWS_ACCESS_KEY_ID=your_access_key
export AWS_SECRET_ACCESS_KEY=your_secret_key

go run main.go \
  -connection=mariadb \
  -db-host=localhost \
  -db-port=3306 \
  -db-name=your_database \
  -db-user=your_username \
  -db-password=your_password \
  -path=./backups \
  -s3-bucket=your-bucket-name \
  -s3-region=us-east-1 \
  -s3-prefix=backups/ \
  -interval=3600 \
  -max-files=10 \
  -gzip=true
```

### With HETZNER Object Storage

```bash
export AWS_ACCESS_KEY_ID=your_hetzner_access_key
export AWS_SECRET_ACCESS_KEY=your_hetzner_secret_key

go run main.go \
  -connection=mariadb \
  -db-host=localhost \
  -db-port=3306 \
  -db-name=your_database \
  -db-user=your_username \
  -db-password=your_password \
  -path=./backups \
  -s3-bucket=your-bucket-name \
  -s3-region=hel1 \
  -s3-endpoint=https://hel1.your-objectstorage.com \
  -s3-prefix=backups/ \
  -interval=3600 \
  -max-files=10 \
  -gzip=true
```

## Configuration

You can configure the application using command-line flags or environment variables. Flags take precedence over environment variables.

### Available Options

| Flag | Environment Variable | Description | Default |
|------|----------------------|-------------|---------|
| `-connection` | `DB_CONNECTION` | Database connection type (mysql, mariadb, postgresql, redis) | mariadb |
| `-db-host` | `DB_HOST` | Database host | 127.0.0.1 |
| `-db-port` | `DB_PORT` | Database port | 3306 |
| `-db-name` | `DB_NAME` | Database name (Required for SQL) | |
| `-db-user` | `DB_USER` | Database user (Required for SQL) | |
| `-db-password` | `DB_PASSWORD` | Database password | |
| `-path` | `BACKUP_PATH` | Local backup storage path | ./backups |
| `-s3-bucket` | `S3_BUCKET` | S3 bucket name for backup storage | |
| `-s3-region` | `S3_REGION` | S3 region | |
| `-s3-endpoint` | `S3_ENDPOINT` | S3 custom endpoint URL | |
| `-s3-prefix` | `S3_PREFIX` | S3 object prefix | backups/ |
| `-max-files` | `MAX_FILES` | Maximum number of backup files to keep | 10 |
| `-interval` | `BACKUP_INTERVAL` | Interval in seconds between backups (min 5) | 15 |
| `-gzip` | `GZIP_COMPRESSION` | Compress backup files with gzip | false |
| `-optimize` | `OPTIMIZE_BACKUP` | Optimize backup performance | false |

### Setting Environment Variables

#### Option 1: Shell Export (Linux/macOS)
You can export variables before running the application:

```bash
export DB_HOST=192.168.1.50
export DB_PASSWORD=secure_password
./db-backup
```

#### Option 2: Docker Compose
Set variables in the `environment` section of your `docker-compose.yml`:

```yaml
services:
  db-backup:
    environment:
      - DB_HOST=database
      - DB_PASSWORD=secret
      - S3_BUCKET=my-backups
```

#### Option 3: Systemd Service
Add `Environment` directives to your service file:

```ini
[Service]
Environment=DB_HOST=localhost
Environment=DB_PASSWORD=secret
```

Or use an environment file:

```ini
[Service]
EnvironmentFile=/etc/default/go-db-backup
```

## Building the Executable

```bash
go build -o db-backup main.go
```

Then run:

```bash
./db-backup
```

## Running as a Service

You can run the backup system as a systemd service. Create a service file at `/etc/systemd/system/db-backup.service`:

```ini
[Unit]
Description=Go Database Backup Service
After=network.target mysql.service
Requires=mysql.service

[Service]
Type=simple
User=www-data
WorkingDirectory=/path/to/go-db-backup
Environment=AWS_ACCESS_KEY_ID=your_access_key
Environment=AWS_SECRET_ACCESS_KEY=your_secret_key
Environment=DB_NAME=your_db
Environment=DB_USER=user
Environment=DB_PASSWORD=password
Environment=S3_BUCKET=your-bucket
Environment=S3_REGION=hel1
Environment=S3_ENDPOINT=https://hel1.your-objectstorage.com
ExecStart=/path/to/db-backup
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable db-backup.service
sudo systemctl start db-backup.service
```

## Security Considerations

- Store AWS credentials securely using environment variables
- Ensure backup directories have appropriate permissions
- Use strong passwords for database access
- Limit access to the backup system to authorized personnel only

## Troubleshooting

### Permission Issues
If you encounter permission errors, ensure:
- The user running the backup has read access to the database
- The user has write access to the backup directory
- AWS credentials have the necessary S3 permissions

### Network Issues
If backups to S3 are failing, check:
- Network connectivity to the S3 endpoint
- Correctness of the S3 endpoint URL
- Validity of AWS credentials

## License

MIT