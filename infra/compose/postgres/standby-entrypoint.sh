#!/bin/sh
# Entrada do Postgres RÉPLICA (#52, ADR 0009). Montado em prod-standby.yaml como
# /usr/local/bin/stellytics-standby-entrypoint; a imagem do Postgres não muda.
#
# Com o diretório de dados vazio, copia o primário com pg_basebackup (-R grava o
# standby.signal e o primary_conninfo; -S usa o slot da réplica, criado antes)
# e só então entrega para o docker-entrypoint.sh da imagem, que não roda initdb
# porque já existe PG_VERSION. Com dados, só reinicia como réplica (o
# standby.signal já está lá). Nunca apaga dados: se o diretório não está vazio e
# não tem PG_VERSION, recusa; e recusa também um cluster sem standby.signal.
set -eu

: "${PRIMARY_HOST:?}" "${REPLICATOR_PASSWORD:?}"
primary_port="${PRIMARY_PORT:-5432}"
slot="${REPLICATION_SLOT:-stellytics_standby}"
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
  mkdir -p "$pgdata"
  marker="$pgdata/.standby-bootstrap-in-progress"
  if [ -e "$marker" ]; then
    echo "standby-entrypoint: removendo cópia parcial de uma tentativa anterior"
    find "$pgdata" -mindepth 1 -maxdepth 1 ! -name lost+found -exec rm -rf -- {} +
  elif [ -n "$(find "$pgdata" -mindepth 1 -maxdepth 1 ! -name lost+found 2>/dev/null | head -n 1)" ]; then
    echo "standby-entrypoint: $pgdata não está vazio e não é um cluster; recusando." >&2
    exit 1
  fi
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
  : > "$marker"
  chown postgres:postgres "$marker"
  gosu postgres pg_basebackup \
    -h "$PRIMARY_HOST" -p "$primary_port" -U replicator \
    -D "$pgdata" -X stream -S "$slot" -R -c fast -P
  rm -f "$marker"
fi

# Há cluster mas não há standby.signal: é o diretório de um primário (ou de uma
# réplica já promovida). Subir assim abriria um segundo primário com dados
# divergentes (split-brain), então recusa. Para refazer uma réplica, apague o
# diretório de dados; STANDBY_FORCE=1 existe só para quem sabe o que está fazendo.
if [ -s "$pgdata/PG_VERSION" ] && [ ! -e "$pgdata/standby.signal" ] && [ "${STANDBY_FORCE:-}" != 1 ]; then
  echo "standby-entrypoint: $pgdata tem um cluster sem standby.signal (primário antigo ou réplica já promovida); recusando para não abrir um segundo primário. Apague o diretório para refazer a réplica." >&2
  exit 1
fi

exec docker-entrypoint.sh "$@"
