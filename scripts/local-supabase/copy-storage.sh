#!/usr/bin/env bash
# Copies every file of the app's buckets from Supabase Storage to the local Storage.
# Downloads count as Supabase egress (about the bucket size, ~0.6 GB): run it once, after the
# project's restriction is lifted. Already-copied files are skipped, so it can be re-run.
# Usage: ./copy-storage.sh   (reads SUPABASE_URL / SUPABASE_SERVICE_ROLE_KEY / DATABASE_URL from .env.remote)
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
need docker "https://www.docker.com/products/docker-desktop/"
REMOTE_URL="${REMOTE_SUPABASE_URL:-$(env_value SUPABASE_URL .env.remote)}"
REMOTE_KEY="${REMOTE_SERVICE_ROLE_KEY:-$(env_value SUPABASE_SERVICE_ROLE_KEY .env.remote)}"
REMOTE_DB="${REMOTE_DATABASE_URL:-$(env_value DATABASE_URL .env.remote)}"
[ -n "$REMOTE_URL" ] && [ -n "$REMOTE_KEY" ] && [ -n "$REMOTE_DB" ] || { echo "ต้องมี .env.remote (สำเนา .env ที่ชี้ Supabase ตัวจริง)" >&2; exit 1; }
LOCAL_URL="$(local_value API_URL)"; LOCAL_KEY="$(local_service_key)"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
copied=0; skipped=0; failed=0
for bucket in "${BUCKETS[@]}"; do
  docker run --rm "$PG_IMAGE" psql "$REMOTE_DB" -Atc "select name || '|' || coalesce(metadata->>'mimetype','application/octet-stream') from storage.objects where bucket_id='$bucket' order by name" > "$TMP/list"
  echo "$bucket: $(wc -l < "$TMP/list" | tr -d ' ') ไฟล์"
  while IFS='|' read -r name mime; do
    [ -z "$name" ] && continue
    path="$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1]))' "$name")"
    if [ "$(curl -s -o /dev/null -w '%{http_code}' -I "$LOCAL_URL/storage/v1/object/$bucket/$path" -H "Authorization: Bearer $LOCAL_KEY")" = 200 ]; then skipped=$((skipped+1)); continue; fi
    if curl -sf "$REMOTE_URL/storage/v1/object/$bucket/$path" -H "Authorization: Bearer $REMOTE_KEY" -H "apikey: $REMOTE_KEY" -o "$TMP/file" \
      && curl -sf -X POST "$LOCAL_URL/storage/v1/object/$bucket/$path" -H "Authorization: Bearer $LOCAL_KEY" -H "Content-Type: $mime" -H 'x-upsert: true' --data-binary @"$TMP/file" -o /dev/null; then
      copied=$((copied+1))
    else
      failed=$((failed+1)); echo "  ไม่สำเร็จ: $bucket/$name" >&2
    fi
  done < "$TMP/list"
done
echo "คัดลอก $copied ไฟล์ · มีอยู่แล้ว $skipped · ไม่สำเร็จ $failed"
