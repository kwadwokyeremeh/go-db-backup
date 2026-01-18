#!/bin/bash

# Build script for Go Database Backup System
set -e

echo "Building Go Database Backup System..."

# Create builds directory
mkdir -p builds

# Build for Linux AMD64 (most common server platform)
echo "Building for Linux AMD64..."
GOOS=linux GOARCH=amd64 go build -o builds/db-backup-linux-amd64 -ldflags="-s -w" main.go

# Build for Linux ARM64 (for ARM servers like Raspberry Pi, AWS Graviton)
echo "Building for Linux ARM64..."
GOOS=linux GOARCH=arm64 go build -o builds/db-backup-linux-arm64 -ldflags="-s -w" main.go

# Build for macOS AMD64
echo "Building for macOS AMD64..."
GOOS=darwin GOARCH=amd64 go build -o builds/db-backup-darwin-amd64 -ldflags="-s -w" main.go

# Build for macOS ARM64 (Apple Silicon)
echo "Building for macOS ARM64..."
GOOS=darwin GOARCH=arm64 go build -o builds/db-backup-darwin-arm64 -ldflags="-s -w" main.go

# Build for Windows AMD64
echo "Building for Windows AMD64..."
GOOS=windows GOARCH=amd64 go build -o builds/db-backup-windows-amd64.exe -ldflags="-s -w" main.go

# Make binaries executable (except Windows .exe which doesn't need chmod)
echo "Making binaries executable..."
chmod +x builds/db-backup-linux-amd64
chmod +x builds/db-backup-linux-arm64
chmod +x builds/db-backup-darwin-amd64
chmod +x builds/db-backup-darwin-arm64

echo "Builds completed successfully!"
echo "Binaries are located in the 'builds' directory:"

ls -la builds/