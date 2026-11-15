#!/bin/sh
# Entrada do Postgres RÉPLICA (#52, ADR 0009). Montado em prod-standby.yaml como
# /usr/local/bin/notify-standby-entrypoint; a imagem do Postgres não muda.
#
# Com o diretório de dados vazio, copia o primário com pg_basebackup (-R grava o
# standby.signal e o primary_conninfo; -S usa o slot da réplica, criado antes)
# e só então entrega para o docker-entrypoint.sh da imagem, que não roda initdb
# porque já existe PG_VERSION. Com dados, só reinicia como réplica (o
# standby.signal já está lá). Nunca apaga dados: se o diretório não está vazio e
# não tem PG_VERSION, recusa.
set -eu

: "${PRIMARY_HOST:?}" "${REPLICATOR_PASSWORD:?}"
primary_port="${PRIMARY_PORT:-5432}"
slot="${REPLICATION_SLOT:-notify_standby}"
# Senha por arquivo (0600, fora do PGDATA): nem o pg_basebackup nem o
# receptor de WAL (que lê PGPASSFILE do ambiente do contêiner) a recebem por
# argumento, e ela não vai para o primary_conninfo.
export PGPASSFILE="${PGPASSFILE:-/var/lib/postgresql/replicator.pgpass}"
passfile="$PGPASSFILE"
pgdata="${PGDATA:-/var/lib/postgresql/data}"

if [ "$(id -u)" != 0 ]; then
  echo "standby-entrypoint: precisa rodar como root (troca para postgres depois)." >&2
  exit 1
fi

# O arquivo vive na camada do contêiner: é refeito a cada partida.
umask 077
printf '%s:%s:*:replicator:%s\n' "$PRIMARY_HOST" "$primary_port" "$REPLICATOR_PASSWORD" > "$passfile"
chown postgres:postgres "$passfile"

if [ ! -s "$pgdata/PG_VERSION" ]; then
  if [ -n "$(find "$pgdata" -mindepth 1 -maxdepth 1 ! -name lost+found 2>/dev/null | head -n 1)" ]; then
    echo "standby-entrypoint: $pgdata não está vazio e não é um cluster; recusando." >&2
    exit 1
  fi
  mkdir -p "$pgdata"
  chown postgres:postgres "$pgdata"
  chmod 0700 "$pgdata"

  echo "standby-entrypoint: esperando o primário em $PRIMARY_HOST:$primary_port"
  i=0
  until pg_isready -q -h "$PRIMARY_HOST" -p "$primary_port"; do
    i=$((i + 1))
    [ "$i" -le 60 ] || { echo "standby-entrypoint: primário não respondeu em 5 min" >&2; exit 1; }
    sleep 5
  done

  # O slot é criado aqui (idempotente) para o primário só guardar WAL depois que
  # há réplica; num primário recém-promovido ele ainda não existe.
  gosu postgres pg_receivewal -h "$PRIMARY_HOST" -p "$primary_port" -U replicator \
    -S "$slot" --create-slot --if-not-exists

  echo "standby-entrypoint: pg_basebackup do primário"
  gosu postgres pg_basebackup \
    -h "$PRIMARY_HOST" -p "$primary_port" -U replicator \
    -D "$pgdata" -X stream -S "$slot" -R -c fast -P
fi

exec docker-entrypoint.sh "$@"
