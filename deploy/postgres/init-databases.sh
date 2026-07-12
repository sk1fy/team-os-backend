#!/usr/bin/env bash
set -Eeuo pipefail

# One Postgres cluster is inexpensive in development, while separate databases
# preserve service ownership and allow a database to move with its service.
for database in \
  teamos_company \
  teamos_kb \
  teamos_tasks \
  teamos_academy \
  teamos_notifications; do
  psql --variable=ON_ERROR_STOP=1 \
    --username "$POSTGRES_USER" \
    --dbname "$POSTGRES_DB" \
    --set=database="$database" <<-'SQL'
	SELECT format('CREATE DATABASE %I', :'database')
	WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'database') \gexec
SQL
done
