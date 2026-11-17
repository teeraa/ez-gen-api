#!/usr/bin/env bash
# Shared settings for the local Supabase scripts. Run them from anywhere; paths resolve from here.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
API_DIR="$(cd "$HERE/../.." && pwd)"
# The Supabase CLI project (config.toml, local volumes) lives outside the API source tree.
WORKDIR="${LOCAL_SUPABASE_DIR:-$API_DIR/.local-supabase}"
BUCKETS=(violet-media violet-characters)
LOCAL_DB_URL="postgresql://postgres:postgres@127.0.0.1:54322/postgres"
# The same database as seen from inside a Docker container (Docker Desktop on macOS).
DOCKER_DB_URL="postgresql://postgres:postgres@host.docker.internal:54322/postgres"
PG_IMAGE="${PG_IMAGE:-postgres:17}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "ต้องติดตั้ง $1 ก่อน: $2" >&2; exit 1; }; }

# Reads a key from ez-gen-api/.env without sourcing it (values may contain shell characters).
env_value() { grep -E "^$1=" "$API_DIR/${2:-.env}" 2>/dev/null | head -1 | cut -d= -f2- ; }

local_status() { (cd "$WORKDIR" && supabase status -o env 2>/dev/null) ; }
local_value() { local_status | grep -E "^$1=" | head -1 | cut -d= -f2- | tr -d '"' ; }
# Service key for Storage: the JWT service_role key (newer CLIs also print SECRET_KEY).
local_service_key() { local v; v="$(local_value SERVICE_ROLE_KEY)"; [ -n "$v" ] || v="$(local_value SECRET_KEY)"; printf %s "$v"; }
