#!/usr/bin/env bash
# Copies the remote database (schema public: tables + data) into the local Postgres.
# Usage: REMOTE_DATABASE_URL=postgresql://... ./import-db.sh
#        (defaults to DATABASE_URL in ez-gen-api/.env.remote, then .env)
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
need docker "https://www.docker.com/products/docker-desktop/"
REMOTE="${REMOTE_DATABASE_URL:-$(env_value DATABASE_URL .env.remote)}"
REMOTE="${REMOTE:-$(env_value DATABASE_URL)}"
case "$REMOTE" in *127.0.0.1*|*localhost*|"") echo "ตั้ง REMOTE_DATABASE_URL เป็นฐานข้อมูล Supabase ตัวจริง" >&2; exit 1;; esac
# The local database must be up first, otherwise the restore below would silently do nothing.
docker run --rm "$PG_IMAGE" pg_isready -q -d "$DOCKER_DB_URL" || { echo "ยังไม่ได้เปิด Supabase local — รัน ./scripts/local-supabase/start.sh ก่อน" >&2; exit 1; }
DUMP="$WORKDIR/remote-public.dump"
mkdir -p "$WORKDIR"
echo "1/2 ดึงฐานข้อมูลจาก Supabase (schema public)…"
docker run --rm -v "$WORKDIR:/out" "$PG_IMAGE" pg_dump "$REMOTE" --schema=public --no-owner --no-privileges -Fc -f /out/remote-public.dump
echo "2/2 นำเข้าฐานข้อมูล local…"
# --clean drops the local copies first, so the import can be re-run.
docker run --rm -v "$WORKDIR:/out" "$PG_IMAGE" pg_restore --clean --if-exists --no-owner --no-privileges -d "$DOCKER_DB_URL" /out/remote-public.dump || true
echo "เสร็จแล้ว (ไฟล์ dump: $DUMP) ถ้ามีคำเตือนเรื่อง extension/role ด้านบนเป็นเรื่องปกติ"
docker run --rm "$PG_IMAGE" psql "$DOCKER_DB_URL" -Atc "select count(*) || ' tables' from information_schema.tables where table_schema='public'"
