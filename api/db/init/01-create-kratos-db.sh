#!/bin/bash
set -e

# Create a dedicated `kratos` role that owns the `kratos` database, per
# Ory's production guide. Least-privilege isolation: a compromised Kratos
# process can't reach the invosit application database.
# https://www.ory.com/docs/kratos/guides/deploy-kratos-example
#
# Idempotent — the docker-entrypoint only runs init scripts on first boot,
# but the guards keep this safe to re-run manually if you ever shell in.
: "${KRATOS_DB_PASSWORD:?KRATOS_DB_PASSWORD must be set}"

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
  DO \$\$
  BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'kratos') THEN
      CREATE ROLE kratos LOGIN PASSWORD '$KRATOS_DB_PASSWORD';
    END IF;
  END
  \$\$;

  SELECT 'CREATE DATABASE kratos OWNER kratos'
  WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'kratos')\gexec
EOSQL
