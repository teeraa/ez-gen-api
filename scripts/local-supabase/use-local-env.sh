#!/usr/bin/env bash
# Switches ez-gen-api/.env to the local Supabase; the original is kept as .env.remote.
# ./use-local-env.sh remote  → switches back.
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
cd "$API_DIR"
if [ "${1:-}" = remote ]; then
  [ -f .env.remote ] || { echo "ไม่มี .env.remote" >&2; exit 1; }
  cp .env .env.local-supabase && cp .env.remote .env && echo "กลับไปใช้ Supabase ตัวจริงแล้ว (restart API)"; exit 0
fi
API_URL="$(local_value API_URL)"; SERVICE_KEY="$(local_service_key)"
[ -n "$SERVICE_KEY" ] || { echo "ยังไม่ได้เปิด Supabase local — รัน ./scripts/local-supabase/start.sh ก่อน" >&2; exit 1; }
[ -f .env.remote ] || cp .env .env.remote
python3 - "$LOCAL_DB_URL" "$API_URL" "$SERVICE_KEY" <<'PY'
import sys, re
db, url, key = sys.argv[1:]
lines = open(".env.remote").read().splitlines()
values = {"DATABASE_URL": db, "SUPABASE_URL": url, "SUPABASE_SERVICE_ROLE_KEY": key}
out = []
for line in lines:
    k = line.split("=", 1)[0]
    out.append(f"{k}={values.pop(k)}" if k in values else line)
out += [f"{k}={v}" for k, v in values.items()]
open(".env", "w").write("\n".join(out) + "\n")
PY
echo "ใช้ Supabase local แล้ว (.env เดิมเก็บไว้ที่ .env.remote) — restart API"
