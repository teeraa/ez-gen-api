#!/usr/bin/env bash
# Starts a local Supabase (Postgres + Storage) in Docker and creates the app's buckets.
# Ports: API/Storage 54321, Postgres 54322, Studio http://127.0.0.1:54323
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
need docker "https://www.docker.com/products/docker-desktop/"
need supabase "brew install supabase/tap/supabase"
docker info >/dev/null 2>&1 || { echo "เปิด Docker Desktop ก่อน" >&2; exit 1; }

mkdir -p "$WORKDIR"
if [ ! -f "$WORKDIR/supabase/config.toml" ]; then
  (cd "$WORKDIR" && supabase init --force >/dev/null)
fi
(cd "$WORKDIR" && supabase start -x realtime,edge-runtime,logflare,vector,imgproxy,supavisor)

API_URL="$(local_value API_URL)"; SERVICE_KEY="$(local_service_key)"
for bucket in "${BUCKETS[@]}"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API_URL/storage/v1/bucket" \
    -H "Authorization: Bearer $SERVICE_KEY" -H "apikey: $SERVICE_KEY" -H 'Content-Type: application/json' \
    -d "{\"id\":\"$bucket\",\"name\":\"$bucket\",\"public\":false}")
  case "$code" in 200) echo "สร้าง bucket $bucket แล้ว";; 400|409) echo "มี bucket $bucket อยู่แล้ว";; *) echo "สร้าง bucket $bucket ไม่สำเร็จ (HTTP $code)" >&2;; esac
done

cat <<MSG

Local Supabase พร้อมแล้ว ใส่ค่าเหล่านี้ใน ez-gen-api/.env (สำรอง .env เดิมไว้ก่อน):
DATABASE_URL=$LOCAL_DB_URL
SUPABASE_URL=$API_URL
SUPABASE_SERVICE_ROLE_KEY=$SERVICE_KEY
หรือรัน: ./scripts/local-supabase/use-local-env.sh
MSG
