#!/usr/bin/env bash
# Moves the local Supabase (schema public + Storage buckets) into a NEW Supabase Cloud project.
# The destination must be empty. Local stays untouched. Already-uploaded files are skipped, so
# step 2 can be re-run.
#
# Usage (values from the new project's dashboard):
#   NEW_SUPABASE_URL=https://<ref>.supabase.co \
#   NEW_SERVICE_ROLE_KEY=<service_role / secret key> \
#   NEW_DATABASE_URL='postgresql://postgres.<ref>:<password>@<session-pooler-host>:5432/postgres?sslmode=require' \
#   ./scripts/local-supabase/push-to-cloud.sh [db|storage|all]
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
need docker "https://www.docker.com/products/docker-desktop/"
STEP="${1:-all}"
: "${NEW_SUPABASE_URL:?ตั้ง NEW_SUPABASE_URL}" "${NEW_SERVICE_ROLE_KEY:?ตั้ง NEW_SERVICE_ROLE_KEY}" "${NEW_DATABASE_URL:?ตั้ง NEW_DATABASE_URL}"
case "$NEW_DATABASE_URL" in *127.0.0.1*|*localhost*) echo "NEW_DATABASE_URL ต้องเป็นโปรเจกต์ Cloud" >&2; exit 1;; esac
NEW_SUPABASE_URL="${NEW_SUPABASE_URL%/}"
docker run --rm "$PG_IMAGE" pg_isready -q -d "$DOCKER_DB_URL" || { echo "ยังไม่ได้เปิด Supabase local — รัน ./scripts/local-supabase/start.sh ก่อน" >&2; exit 1; }
psql_local() { docker run --rm "$PG_IMAGE" psql "$DOCKER_DB_URL" -Atc "$1"; }
psql_new()   { docker run --rm "$PG_IMAGE" psql "$NEW_DATABASE_URL" -Atc "$1"; }

if [ "$STEP" = db ] || [ "$STEP" = all ]; then
  existing="$(psql_new "select count(*) from information_schema.tables where table_schema='public'")"
  if [ "$existing" != 0 ] && [ "${FORCE:-}" != 1 ]; then
    echo "โปรเจกต์ใหม่มีตารางใน public อยู่แล้ว $existing ตาราง — หยุดไว้ก่อน (FORCE=1 เพื่อเขียนทับ)" >&2; exit 1
  fi
  mkdir -p "$WORKDIR"
  echo "DB 1/2 dump local (schema public)…"
  docker run --rm -v "$WORKDIR:/out" "$PG_IMAGE" pg_dump "$DOCKER_DB_URL" --schema=public --no-owner --no-privileges -Fc -f /out/local-public.dump
  echo "DB 2/2 restore ไปโปรเจกต์ใหม่…"
  docker run --rm -v "$WORKDIR:/out" "$PG_IMAGE" pg_restore --clean --if-exists --no-owner --no-privileges -d "$NEW_DATABASE_URL" /out/local-public.dump || true
  echo "local: $(psql_local "select count(*) from information_schema.tables where table_schema='public'") ตาราง · ใหม่: $(psql_new "select count(*) from information_schema.tables where table_schema='public'") ตาราง"
fi

if [ "$STEP" = storage ] || [ "$STEP" = all ]; then
  LOCAL_URL="$(local_value API_URL)"; LOCAL_KEY="$(local_service_key)"
  TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
  copied=0; skipped=0; failed=0
  for bucket in "${BUCKETS[@]}"; do
    # Same settings as the local bucket (always private for this app).
    code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$NEW_SUPABASE_URL/storage/v1/bucket" \
      -H "Authorization: Bearer $NEW_SERVICE_ROLE_KEY" -H "apikey: $NEW_SERVICE_ROLE_KEY" -H 'Content-Type: application/json' \
      -d "{\"id\":\"$bucket\",\"name\":\"$bucket\",\"public\":false}")
    case "$code" in 200) echo "สร้าง bucket $bucket แล้ว";; 400|409) echo "มี bucket $bucket อยู่แล้ว";; *) echo "สร้าง bucket $bucket ไม่สำเร็จ (HTTP $code)" >&2; exit 1;; esac
    psql_local "select name || '|' || coalesce(metadata->>'mimetype','application/octet-stream') from storage.objects where bucket_id='$bucket' order by name" > "$TMP/list"
    echo "$bucket: $(wc -l < "$TMP/list" | tr -d ' ') ไฟล์"
    while IFS='|' read -r name mime; do
      [ -z "$name" ] && continue
      path="$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1]))' "$name")"
      if [ "$(curl -s -o /dev/null -w '%{http_code}' -I "$NEW_SUPABASE_URL/storage/v1/object/$bucket/$path" -H "Authorization: Bearer $NEW_SERVICE_ROLE_KEY" -H "apikey: $NEW_SERVICE_ROLE_KEY")" = 200 ]; then skipped=$((skipped+1)); continue; fi
      if curl -sf "$LOCAL_URL/storage/v1/object/$bucket/$path" -H "Authorization: Bearer $LOCAL_KEY" -o "$TMP/file" \
        && curl -sf -X POST "$NEW_SUPABASE_URL/storage/v1/object/$bucket/$path" -H "Authorization: Bearer $NEW_SERVICE_ROLE_KEY" -H "apikey: $NEW_SERVICE_ROLE_KEY" -H "Content-Type: $mime" -H 'x-upsert: true' --data-binary @"$TMP/file" -o /dev/null; then
        copied=$((copied+1))
      else
        failed=$((failed+1)); echo "  ไม่สำเร็จ: $bucket/$name ($(du -h "$TMP/file" 2>/dev/null | cut -f1))" >&2
      fi
    done < "$TMP/list"
  done
  echo "อัปโหลด $copied ไฟล์ · มีอยู่แล้ว $skipped · ไม่สำเร็จ $failed"
fi
