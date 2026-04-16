#!/bin/sh
# Run only the Up section of each goose migration file.
# docker-entrypoint-initdb.d executes plain SQL, which would also run Down sections.
# This script extracts only lines between "-- +goose Up" and "-- +goose Down".
set -e

MIGRATION_DIR="/migrations"

for f in "$MIGRATION_DIR"/*.sql; do
    echo "Applying migration: $f"
    # Extract lines from start (or after "-- +goose Up") up to (but not including) "-- +goose Down"
    awk '/^-- \+goose Down/{exit} /^-- \+goose Up/{found=1; next} found{print}' "$f" \
        | psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB"
done
