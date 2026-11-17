# Supabase บนเครื่อง (Postgres + Storage ใน Docker)

ใช้แทนโปรเจกต์ Supabase ตัวจริงระหว่างพัฒนา จะได้ไม่กินโควตา egress
API เรียก Storage ผ่าน endpoint แบบเดียวกับ Supabase จึงไม่ต้องแก้โค้ด เปลี่ยนแค่ค่าใน `.env`

## ติดตั้งครั้งแรก
1. Docker Desktop และ Supabase CLI: `brew install supabase/tap/supabase`
2. `./scripts/local-supabase/start.sh` — เปิด Postgres (54322), Storage (54321), Studio http://127.0.0.1:54323 และสร้าง bucket `violet-media`, `violet-characters`
3. `./scripts/local-supabase/use-local-env.sh` — สลับ `.env` ไปใช้ local (`.env` เดิมเก็บไว้ที่ `.env.remote`)
4. `./scripts/local-supabase/import-db.sh` — ดึงฐานข้อมูลตัวจริง (schema `public` ทั้งตารางและข้อมูล ~40 MB) มาใส่ local
5. restart API (`go run main.go`)

## ไฟล์ใน Storage
`./scripts/local-supabase/copy-storage.sh` คัดลอกไฟล์ทั้งสอง bucket (~0.6 GB) มาเครื่อง
ต้องรอให้โปรเจกต์ Supabase พ้นสถานะถูกจำกัด (อัปเกรด หรือขึ้นรอบบิลใหม่) เพราะตอนนี้ดาวน์โหลดไม่ได้ (402)
และการดาวน์โหลดนับเป็น egress ครั้งเดียว ~0.6 GB รันซ้ำได้ ไฟล์ที่คัดลอกแล้วจะข้าม

## ข้อจำกัด
- ลิงก์ไฟล์จะเป็น `http://127.0.0.1:54321/...` เปิดได้จาก browser ในเครื่องนี้เท่านั้น
- งานที่ส่งภาพหรือวิดีโอให้ BytePlus (Seedream/Seedance) ใช้ภาพอ้างอิงจาก local ไม่ได้ เพราะเซิร์ฟเวอร์ BytePlus เข้า 127.0.0.1 ไม่ถึง
  ถ้าต้องสร้างภาพ/วิดีโอ ให้เปิด tunnel เช่น `cloudflared tunnel --url http://127.0.0.1:54321` แล้วตั้ง `SUPABASE_URL` เป็น URL ของ tunnel
- กลับไปใช้ตัวจริง: `./scripts/local-supabase/use-local-env.sh remote`
