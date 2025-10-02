#!/bin/bash
# Build script for weblet with version as build date/time

# Generate simplified version: <days_since_epoch>.<HHMM>
# Date: Days since Jan 1, 2024 (3-4 digits)
# Time: HHMM format (4 digits)
EPOCH_DATE="2024-01-01"
CURRENT_DATE=$(date +%Y-%m-%d)
DAYS_SINCE_EPOCH=$(( ( $(date -d "$CURRENT_DATE" +%s) - $(date -d "$EPOCH_DATE" +%s) ) / 86400 ))
TIME_HHMM=$(date +%H%M)
VERSION="${DAYS_SINCE_EPOCH}.${TIME_HHMM}"

# Build the binary with version embedded
go build -ldflags "-X main.version=$VERSION" -o weblet

echo "Built weblet with version: $VERSION"

