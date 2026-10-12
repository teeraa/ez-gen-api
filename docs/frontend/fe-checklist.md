# Frontend checklist — ตามการเปลี่ยนแปลงของ ez-gen-api (2026-10-12)

เอกสารนี้สรุปสิ่งที่ `ez-gen-web` ต้องทำให้ตรงกับ backend ล่าสุดบน `main`
รายละเอียด field ทั้งหมดอยู่ใน `docs/engineering/video-generation-api-contract.md` และ
`docs/engineering/violet-dna-api-contract.md` (backend เป็นตัวจริง)

ทุก response ห่อด้วย envelope `{ "data": ..., "requestId": "..." }` อ่านค่าจาก `data` เสมอ

---

## 1. Polling งาน (เร่งด่วน — ตอนนี้ยิง ~10 req/s)

- [ ] มี polling loop **เดียว** ต่อ job ID (ใช้ ref/flag กันสร้างซ้ำตอน re-render หรือ StrictMode)
- [ ] Poll `GET /open/v1/jobs/:jobId` ทุก **2–3 วินาที** ไม่ถี่กว่านี้
- [ ] `clearInterval` / abort ใน cleanup ของ `useEffect` เมื่อ unmount หรือ jobId เปลี่ยน
- [ ] หยุด poll ทันทีเมื่อ `status` เป็น `succeeded`, `failed` หรือ `cancelled`
- [ ] เรียก `PUT /open/v1/dna/scenes/:id/select-output/:jobId` **ครั้งเดียว** ต่อ job (ตอนนี้ยิงซ้ำหลายร้อยครั้ง)
- [ ] ไม่ให้ผลจาก `select-output` ไป trigger การ poll ใหม่ (ตรวจ dependency array ของ effect)

## 2. Progress bar

Response ของ job มี field ใหม่:

```json
"progress": 45,
"currentStep": "queued | generating | saving | done | failed | cancelled",
"estimatedSecondsRemaining": 20
```

- [ ] แสดงแถบจาก `data.progress` (0–100)
- [ ] ห้ามถอยหลัง: `shown = Math.max(shown, progress)`
- [ ] ใส่ CSS transition ~1–2 วินาที ให้ลื่นระหว่าง poll
- [ ] ข้อความตาม `currentStep`: `queued` "รอคิว…", `generating` "กำลังสร้างวิดีโอ…", `saving` "กำลังบันทึก…"
- [ ] แสดง "เหลือประมาณ N วินาที" เมื่อ `estimatedSecondsRemaining` ไม่เป็น `null` และ > 0
- [ ] เมื่อ `estimatedSecondsRemaining === 0` แต่ยังไม่เสร็จ แสดง "ใกล้เสร็จแล้ว…" แทนตัวเลข
- [ ] ขึ้น 100% เฉพาะเมื่อ `status === "succeeded"`
- [ ] รองรับ `status: "submitted"` (อยู่ระหว่าง queued กับ processing) ให้แสดงเหมือนกำลังทำงาน

## 3. เล่นวิดีโอ

- [ ] ใช้ `data.result.mediaUrl` เป็น `<video src={...} controls playsInline>` ตรง ๆ
- [ ] **ไม่ใส่** `crossOrigin` และไม่โหลดผ่าน `fetch()`/blob
- [ ] Thumbnail จาก `data.result.thumbnailUrl` — อาจเป็น `""` ต้องมี fallback/placeholder
- [ ] URL เป็น signed URL ของ Supabase (`https://<ref>.supabase.co/storage/v1/object/sign/...`) อายุ **1 ชั่วโมง** (`result.expiresAt`)
  - [ ] เพิ่ม `<ref>.supabase.co` ใน CSP (`media-src`, `img-src`) ถ้ามี
  - [ ] เพิ่มใน `next.config` → `images.remotePatterns` ถ้าใช้ `next/image` กับ thumbnail
- [ ] ห้าม cache URL ถาวร (localStorage/DB ของ FE) — เก็บ `jobId` แล้วขอ URL ใหม่
- [ ] เมื่อ `<video onError>` หรือเลย `expiresAt` → เรียก `GET /jobs/:jobId` ใหม่แล้วอัปเดต `src`
- [ ] หน้า scene: ได้แค่ `selectedJobId` → ต้องเรียก `GET /jobs/{selectedJobId}` เพื่อเอา `mediaUrl`

## 4. ข้อจำกัดของ model (ตอนนี้ทุก model วิ่งบน Seedance 1.0 Pro)

ถ้าเกินจะได้ `422 MODEL_NOT_SUPPORTED`

- [ ] ความยาววิดีโอ: ให้เลือกได้ **4–12 วินาที** (เอา 15/30 ออก)
- [ ] Aspect ratio: `16:9`, `9:16`, `1:1`, `4:3`, `3:4`, `21:9`
- [ ] ซ่อน/ปิดโหมด `image_image` (reference image) — model นี้ไม่รองรับ
- [ ] ซ่อน toggle/ป้าย "มีเสียง" (ไม่มี audio)
- [ ] (ไม่บังคับ) เพิ่ม `violet-classic` ในตัวเลือก model
- [ ] แสดงข้อความที่เข้าใจได้เมื่อได้ `MODEL_NOT_SUPPORTED` เช่น "ความยาวหรือสัดส่วนนี้ไม่รองรับในโมเดลปัจจุบัน"

> ข้อจำกัดเปลี่ยนตาม `ARK_DEFAULT_MODEL` ของ backend ถ้าต้องการไม่ hardcode ให้ขอ endpoint `GET /open/v1/models`

## 5. สถานะ/error ใหม่ของ job

- [ ] `error.code === "OUTPUT_EXPIRED"` → "บันทึกวิดีโอไม่ทัน กรุณาสร้างใหม่" + ปุ่มสร้างใหม่
- [ ] `status` อาจอยู่ `processing` (step `saving`) นานขึ้นหลัง provider ทำเสร็จ เพราะ backend กำลัง copy ไฟล์เข้า storage

## 6. Continue Scene (ใช้งานได้แล้ว)

- [ ] `POST /open/v1/dna/scenes/:id/continue` กับ `useLastFrame: true` ใช้เฟรมสุดท้ายของ output ที่เลือกไว้เป็นเฟรมแรก
- [ ] ต้องเลือก output ของซีนก่อนหน้าแล้ว (`select-output`) และ job นั้นต้อง `succeeded` — ไม่งั้น backend จะสร้างแบบ text-only โดยไม่แจ้ง error
- [ ] ซีนใหม่ได้ตัวละคร **ทุกตัว** จากซีนก่อน (ตามลำดับเดิม) — refresh ข้อมูลซีนหลัง continue

## 7. หลายตัวละครต่อซีน (สูงสุด 3)

### สร้าง/แก้ซีน
- [ ] UI เลือกตัวละครได้หลายตัว **สูงสุด 3** และเรียงลำดับได้ (ตัวแรก = ตัวหลัก)
- [ ] ส่ง `characterProfileIds: string[]` ตามลำดับที่ผู้ใช้จัด
  - `POST /open/v1/dna/scenes` — ส่ง list
  - `PUT /open/v1/dna/scenes/:id` — ส่ง list = **แทนที่ทั้งหมด**, ส่ง `[]` = ล้าง, ไม่ส่ง field = ไม่เปลี่ยน
- [ ] เลิกส่ง `characterProfileId` เดี่ยวในโค้ดใหม่ (backend ยังรับอยู่ช่วงเปลี่ยนผ่าน แต่ถ้าส่งใน `PUT` จะแทนที่ list ทั้งหมดด้วยตัวเดียว)
- [ ] ห้ามมี ID ซ้ำใน list
- [ ] Handle error:
  - `400 VALIDATION_ERROR` (`details[].field === "characterProfileIds"`) — เกิน 3 / ซ้ำ / ไม่ใช่ UUID
  - `404 NOT_FOUND` — มีตัวละครที่ไม่ใช่ของผู้ใช้หรือถูกลบ → reload รายการตัวละคร

### แสดงผลซีน
- [ ] อ่าน `data.characters: [{ id, name, sortOrder }]` (เป็น array เสมอ, เรียงแล้ว) แทน `characterProfileId`
- [ ] แสดง avatar/ชื่อตัวละครทุกตัวในการ์ดซีนและรายการซีน (`GET /dna/scenes` ส่ง `characters` มาด้วยแล้ว)
- [ ] `characterProfileId` ยังมี (= ตัวแรก) แต่ถือว่า deprecated

### สร้างวิดีโอ
- [ ] `POST /open/v1/dna/scenes/:id/generate` ไม่ต้องส่งตัวละคร — backend ใช้ตัวละครทั้งหมดของซีน
- [ ] ส่ง `characterProfileIds` ใน body เฉพาะเมื่ออยากใช้ชุดอื่นเฉพาะ job นี้ (ไม่เปลี่ยนซีน)
- [ ] ช่อง action/prompt: แนะนำผู้ใช้ให้ **เรียกตัวละครด้วยชื่อ** เช่น "Elena ยื่นข้าวเหนียวให้ Somchai" (placeholder/hint) เพื่อให้ model จับคู่คำบรรยายกับคนถูก
- [ ] แสดงคำเตือนเบา ๆ เมื่อเลือก 3 ตัว: "ตัวละครหลายตัวอาจสลับลักษณะกันได้" (model ปัจจุบันใช้ข้อความอย่างเดียว ไม่มีรูปอ้างอิง)

## 8. เอกสาร

- [ ] อัปเดต `ez-gen-web/docs/engineering/api-contract.md` ให้ตรงกับ:
  - `progress`, `currentStep`, `estimatedSecondsRemaining`
  - `result.expiresAt` / อายุ URL 1 ชม.
  - `OUTPUT_EXPIRED`
  - เงื่อนไข `MODEL_NOT_SUPPORTED`
  - `violet-classic`
  - `characterProfileIds` (request) และ `characters` (scene response)
