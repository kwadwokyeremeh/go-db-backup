package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	_ "github.com/go-sql-driver/mysql" // MySQL driver
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // PostgreSQL driver
)

// BackupConfig holds the configuration for the backup process
type BackupConfig struct {
	Connection string
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	Path       string
	S3Bucket   string
	S3Region   string
	S3Endpoint string
	S3Prefix   string
	MaxFiles   int
	Interval   time.Duration
	Gzip       bool
	Optimize   bool
}

// BackupManager handles the backup operations
type BackupManager struct {
	config *BackupConfig
	s3Svc  *s3.Client
	db     *sqlx.DB
}

// NewBackupManager creates a new backup manager
func NewBackupManager(configData *BackupConfig) (*BackupManager, error) {
	bm := &BackupManager{
		config: configData,
	}

	// Initialize S3 client if S3 configuration is provided
	if configData.S3Bucket != "" {
		// Load default config
		cfg, err := config.LoadDefaultConfig(context.TODO(),
			config.WithRegion(configData.S3Region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				"",
			)),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to load AWS config: %v", err)
		}

		// Configure custom endpoint if provided
		if configData.S3Endpoint != "" {
			// For AWS SDK v2, we need to use a custom endpoint resolver
			// Note: In newer v2 versions, BaseEndpoint is the preferred way
			cfg.BaseEndpoint = aws.String(configData.S3Endpoint)
		}

		bm.s3Svc = s3.NewFromConfig(cfg)
	}

	// Connect to the database
	// Map "mariadb" to "mysql" driver as sqlx/go-sql-driver uses "mysql" for both
	driverName := configData.Connection
	if driverName == "mariadb" {
		driverName = "mysql"
	}

	// Only connect to SQL database if not using Redis
	if configData.Connection != "redis" {
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", configData.DBUser, configData.DBPassword, configData.DBHost, configData.DBPort, configData.DBName)
		db, err := sqlx.Connect(driverName, dsn)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to database: %v", err)
		}
		bm.db = db
	}

	return bm, nil
}

// Run starts the continuous backup process
func (bm *BackupManager) Run() error {
	log.Printf("Starting high-frequency database backup for connection: %s", bm.config.Connection)
	log.Printf("Backup path: %s", bm.config.Path)
	log.Printf("Interval: %v", bm.config.Interval)
	log.Printf("Max files to keep: %d", bm.config.MaxFiles)
	log.Printf("Compression: %t", bm.config.Gzip)
	log.Printf("Using S3: %t", bm.config.S3Bucket != "")

	// Ensure backup directory exists
	if err := os.MkdirAll(bm.config.Path, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %v", err)
	}

	counter := 0
	for {
		startTime := time.Now()

		// Generate filename with timestamp
		timestamp := time.Now().Format("2006-01-02_15-04-05")

		var extension string
		if bm.config.Connection == "redis" {
			extension = "rdb"
		} else {
			extension = "sql"
		}

		filename := fmt.Sprintf("backup_%s_%06d.%s", timestamp, counter, extension)
		localPath := filepath.Join(bm.config.Path, filename)

		// Perform the backup
		err := bm.performBackup(localPath)
		if err != nil {
			log.Printf("Backup failed: %v", err)
			time.Sleep(bm.config.Interval)
			continue
		}

		// If compression is enabled, the file will have .gz extension
		checkPath := localPath
		if bm.config.Gzip {
			checkPath += ".gz"
		}

		// Calculate backup size
		size, err := getFileSize(checkPath)
		if err != nil {
			log.Printf("Error getting backup size: %v", err)
		} else {
			duration := time.Since(startTime)
			log.Printf("[%s] Local backup completed in %v, size: %s", timestamp, duration, formatBytes(size))

			// Upload to S3 if configured
			if bm.config.S3Bucket != "" {
				s3StartTime := time.Now()

				s3Key := fmt.Sprintf("%s%s", bm.config.S3Prefix, filepath.Base(checkPath))
				err = bm.uploadToS3(checkPath, s3Key)
				if err != nil {
					log.Printf("Failed to upload to S3: %v", err)
				} else {
					s3Duration := time.Since(s3StartTime)
					log.Printf("[%s] Uploaded to S3 in %v, S3 Key: %s", timestamp, s3Duration, s3Key)

					// Optionally delete local file after successful upload to save space
					os.Remove(checkPath)
				}
			}
		}

		// Clean up old backups
		if bm.config.S3Bucket != "" {
			bm.cleanupOldBackupsS3()
		} else {
			bm.cleanupOldBackups()
		}

		// Sleep for the specified interval
		time.Sleep(bm.config.Interval)
		counter++
	}
}

// performBackup executes the actual database backup
func (bm *BackupManager) performBackup(outputPath string) error {
	var cmd string

	switch bm.config.Connection {
	case "mysql", "mariadb":
		// Check if mariadb-dump exists first
		if _, err := exec.LookPath("mariadb-dump"); err == nil {
			cmd = fmt.Sprintf("mariadb-dump --host=%s --port=%s --user=%s --password=%s --single-transaction --routines --triggers %s",
				bm.config.DBHost, bm.config.DBPort, bm.config.DBUser, bm.config.DBPassword, bm.config.DBName)
		} else if _, err := exec.LookPath("mysqldump"); err == nil {
			// Fallback to mysqldump
			cmd = fmt.Sprintf("mysqldump --host=%s --port=%s --user=%s --password=%s --single-transaction --routines --triggers %s",
				bm.config.DBHost, bm.config.DBPort, bm.config.DBUser, bm.config.DBPassword, bm.config.DBName)
		} else {
			return fmt.Errorf("neither mariadb-dump nor mysqldump found in PATH")
		}
	case "postgres", "postgresql":
		cmd = fmt.Sprintf("pg_dump --host=%s --port=%s --username=%s --dbname=%s",
			bm.config.DBHost, bm.config.DBPort, bm.config.DBUser, bm.config.DBName)
		// Set PGPASSWORD environment variable for pg_dump
		os.Setenv("PGPASSWORD", bm.config.DBPassword)
	case "redis":
		// For Redis, we use redis-cli to trigger a save and then copy the dump file
		// Note: This is a simplified approach. For production Redis, you might want to use BGSAVE
		// and then copy the dump.rdb file, or use --rdb flag if available in newer redis-cli versions.
		// Here we use the --rdb flag which dumps the RDB file to stdout

		// If password is provided, set REDISCLI_AUTH environment variable
		// This avoids the warning about using password on command line
		if bm.config.DBPassword != "" {
			os.Setenv("REDISCLI_AUTH", bm.config.DBPassword)
		}

		// redis-cli --rdb - (dash) writes to stdout
		cmd = fmt.Sprintf("redis-cli -h %s -p %s --rdb -",
			bm.config.DBHost, bm.config.DBPort)

	default:
		return fmt.Errorf("unsupported database connection: %s", bm.config.Connection)
	}

	// Add compression if needed
	if bm.config.Gzip {
		cmd += fmt.Sprintf(" | gzip > %s", outputPath+".gz")
		// Note: We don't update outputPath here because it's passed by value
		// The caller needs to know to look for .gz extension
	} else {
		cmd += fmt.Sprintf(" > %s", outputPath)
	}

	// Add optimization if needed
	if bm.config.Optimize {
		cmd = "nice -n19 ionice -c3 " + cmd
	}

	// Execute the command
	return executeCommand(cmd)
}

// uploadToS3 uploads the backup file to S3
func (bm *BackupManager) uploadToS3(filePath, s3Key string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	_, err = bm.s3Svc.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bm.config.S3Bucket),
		Key:    aws.String(s3Key),
		Body:   file,
	})

	if err != nil {
		return fmt.Errorf("failed to upload to S3: %v", err)
	}

	return nil
}

// cleanupOldBackups removes old backup files locally
func (bm *BackupManager) cleanupOldBackups() {
	files, err := filepath.Glob(filepath.Join(bm.config.Path, "backup_*"))
	if err != nil {
		log.Printf("Error finding backup files: %v", err)
		return
	}

	// Filter files to only include backup files
	var backupFiles []string
	for _, file := range files {
		base := filepath.Base(file)
		if strings.Contains(base, "backup_") && (strings.HasSuffix(base, ".sql") || strings.HasSuffix(base, ".sql.gz") || strings.HasSuffix(base, ".rdb") || strings.HasSuffix(base, ".rdb.gz")) {
			backupFiles = append(backupFiles, file)
		}
	}

	// Sort files by name (which includes timestamp, so chronological order)
	// In a real implementation, you'd want to sort by modification time
	// For simplicity, we'll just remove the oldest files
	if len(backupFiles) <= bm.config.MaxFiles {
		return
	}

	// Sort by name (which contains timestamp)
	// In a real implementation, you'd want to sort by actual timestamp
	// For this example, we'll just remove the first N files that exceed MaxFiles
	for i := 0; i < len(backupFiles)-bm.config.MaxFiles; i++ {
		err := os.Remove(backupFiles[i])
		if err != nil {
			log.Printf("Failed to delete old backup: %v", err)
		} else {
			log.Printf("Deleted old backup: %s", filepath.Base(backupFiles[i]))
		}
	}
}

// cleanupOldBackupsS3 removes old backup files from S3
func (bm *BackupManager) cleanupOldBackupsS3() {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bm.config.S3Bucket),
		Prefix: aws.String(bm.config.S3Prefix),
	}

	result, err := bm.s3Svc.ListObjectsV2(context.TODO(), input)
	if err != nil {
		log.Printf("Failed to list S3 objects: %v", err)
		return
	}

	// Filter for backup files
	var backupObjects []types.Object

	for _, obj := range result.Contents {
		if obj.Key != nil && strings.Contains(*obj.Key, "backup_") {
			key := *obj.Key
			if strings.HasSuffix(key, ".sql") || strings.HasSuffix(key, ".sql.gz") || strings.HasSuffix(key, ".rdb") || strings.HasSuffix(key, ".rdb.gz") {
				backupObjects = append(backupObjects, obj)
			}
		}
	}

	// Sort by LastModified (oldest first)
	// In a real implementation, you'd sort the objects by LastModified
	if len(backupObjects) <= bm.config.MaxFiles {
		return
	}

	// Delete oldest files if we have more than MaxFiles
	for i := 0; i < len(backupObjects)-bm.config.MaxFiles; i++ {
		_, err := bm.s3Svc.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
			Bucket: aws.String(bm.config.S3Bucket),
			Key:    backupObjects[i].Key,
		})

		if err != nil {
			log.Printf("Failed to delete old backup from S3: %v", err)
		} else {
			log.Printf("Deleted old backup from S3: %s", *backupObjects[i].Key)
		}
	}
}

// Helper functions
func getFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func executeCommand(cmd string) error {
	// Split the command to handle pipes properly
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	// For complex commands with pipes, we need to use shell
	cmdObj := exec.Command("/bin/sh", "-c", cmd)

	// Capture stderr to help debug
	cmdObj.Stderr = os.Stderr

	err := cmdObj.Run()
	if err != nil {
		return fmt.Errorf("command failed: %v", err)
	}
	return nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return fallback
}

func main() {
	// Define command-line flags with environment variables as defaults
	var (
		connection = flag.String("connection", getEnv("DB_CONNECTION", "mariadb"), "Database connection to backup")
		dbHost     = flag.String("db-host", getEnv("DB_HOST", "127.0.0.1"), "Database host")
		dbPort     = flag.String("db-port", getEnv("DB_PORT", "3306"), "Database port")
		dbName     = flag.String("db-name", getEnv("DB_NAME", ""), "Database name")
		dbUser     = flag.String("db-user", getEnv("DB_USER", ""), "Database user")
		dbPassword = flag.String("db-password", getEnv("DB_PASSWORD", ""), "Database password")
		path       = flag.String("path", getEnv("BACKUP_PATH", "./backups"), "Backup storage path")
		s3Bucket   = flag.String("s3-bucket", getEnv("S3_BUCKET", ""), "S3 bucket name for backup storage")
		s3Region   = flag.String("s3-region", getEnv("S3_REGION", ""), "S3 region")
		s3Endpoint = flag.String("s3-endpoint", getEnv("S3_ENDPOINT", ""), "S3 custom endpoint URL (for services like HETZNER)")
		s3Prefix   = flag.String("s3-prefix", getEnv("S3_PREFIX", "backups/"), "S3 object prefix")
		maxFiles   = flag.Int("max-files", getEnvInt("MAX_FILES", 10), "Maximum number of backup files to keep")
		interval   = flag.Int("interval", getEnvInt("BACKUP_INTERVAL", 15), "Interval in seconds between backups (min 5 seconds)")
		gzip       = flag.Bool("gzip", getEnvBool("GZIP_COMPRESSION", false), "Compress backup files with gzip")
		optimize   = flag.Bool("optimize", getEnvBool("OPTIMIZE_BACKUP", false), "Optimize backup performance by limiting concurrent operations")
	)

	flag.Parse()

	// Validate required parameters
	// For Redis, DBName and DBUser might not be required
	if *connection != "redis" && (*dbName == "" || *dbUser == "" || *dbPassword == "") {
		log.Fatal("Database name, user, and password are required for SQL databases")
	}

	// Validate interval
	if *interval < 5 {
		log.Fatal("Interval must be at least 5 seconds")
	}

	// Validate S3 configuration if S3 bucket is provided
	if *s3Bucket != "" && *s3Region == "" {
		log.Fatal("S3 region is required when using S3 storage")
	}

	// Set default S3 endpoint if not provided but S3 is configured
	if *s3Bucket != "" && *s3Endpoint == "" {
		*s3Endpoint = fmt.Sprintf("https://s3.%s.amazonaws.com", *s3Region)
	}

	// Create backup config
	config := &BackupConfig{
		Connection: *connection,
		DBHost:     *dbHost,
		DBPort:     *dbPort,
		DBName:     *dbName,
		DBUser:     *dbUser,
		DBPassword: *dbPassword,
		Path:       *path,
		S3Bucket:   *s3Bucket,
		S3Region:   *s3Region,
		S3Endpoint: *s3Endpoint,
		S3Prefix:   *s3Prefix,
		MaxFiles:   *maxFiles,
		Interval:   time.Duration(*interval) * time.Second,
		Gzip:       *gzip,
		Optimize:   *optimize,
	}

	// Create backup manager
	bm, err := NewBackupManager(config)
	if err != nil {
		log.Fatalf("Failed to create backup manager: %v", err)
	}

	// Only close DB if it was initialized
	if bm.db != nil {
		defer bm.db.Close()
	}

	// Start the backup process
	if err := bm.Run(); err != nil {
		log.Fatalf("Backup process failed: %v", err)
	}
}
