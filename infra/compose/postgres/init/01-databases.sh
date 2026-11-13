#!/bin/sh
# Cria um usuário e um banco para cada consumidor: umami (gerenciado pelo Umami)
# e notify (tenants, chaves e métricas de API). Roda só com o volume vazio.
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v umami_pw="$UMAMI_DB_PASSWORD" -v notify_pw="$NOTIFY_DB_PASSWORD" <<'SQL'
CREATE ROLE umami LOGIN PASSWORD :'umami_pw';
CREATE DATABASE umami OWNER umami;
CREATE ROLE notify LOGIN PASSWORD :'notify_pw';
CREATE DATABASE notify OWNER notify;
REVOKE ALL ON DATABASE umami FROM PUBLIC;
REVOKE ALL ON DATABASE notify FROM PUBLIC;
SQL
