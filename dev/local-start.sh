#!/usr/bin/env bash
# Start the local listmonk dev setup (podman Postgres + MailHog, Go app on host).
# See LOCAL_DEV.md.
set -euo pipefail
cd "$(dirname "$0")/.."

start_container() {
  local name=$1; shift
  if podman container exists "$name"; then
    podman start "$name" >/dev/null
  else
    podman run -d --name "$name" "$@" >/dev/null
  fi
  echo "started $name"
}

start_container listmonk-db \
  -p 127.0.0.1:5432:5432 \
  -e POSTGRES_PASSWORD=listmonk-dev -e POSTGRES_USER=listmonk-dev -e POSTGRES_DB=listmonk-dev \
  -v listmonk-dev-db:/var/lib/postgresql \
  docker.io/library/postgres:18

start_container listmonk-mailhog \
  -p 127.0.0.1:1025:1025 -p 127.0.0.1:8025:8025 \
  docker.io/mailhog/mailhog:v1.0.1

until podman exec listmonk-db pg_isready -U listmonk-dev -d listmonk-dev >/dev/null 2>&1; do sleep 1; done

# Build the admin frontend if it hasn't been built yet.
[ -n "$(ls -A static/admin/dist 2>/dev/null)" ] || make build-frontend

export GOTOOLCHAIN=auto
go run ./cmd --config=config.toml --install --idempotent --yes
exec go run ./cmd --config=config.toml
