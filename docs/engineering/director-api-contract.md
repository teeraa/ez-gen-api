# Director (ผู้กำกับ) API Contract — v1 draft

> สถานะ: **Draft · 2026-11-04** — ร่างจาก UI ต้นแบบที่ `ez-gen-web/src/components/director/*` (ตอนนี้ใช้ข้อมูลจำลองใน `localStorage`)
> เจ้าของ: ez-gen-api · ผู้ใช้: ez-gen-web หน้า `/studio/director` และ `/studio/director/[id]`
> ความสัมพันธ์กับ Story Flow: Director คือ Story Flow รุ่นถัดไปที่ **แก้ไขได้ระหว่างทาง** (Story Flow v1 ล็อกข้อมูลหลังสร้าง, `revision` เป็น 1 ตลอด)
> แนะนำให้ใช้ worker, lease, idempotency และ adapter ของ Story Flow ซ้ำ แต่แยก route และตารางใหม่ เพื่อไม่กระทบโปรเจกต์ Story Flow เดิม

---

## 1. ภาพรวม flow

```
[สร้างโปรเจกต์] ─► ① ไอเดีย ──(สร้างเนื้อเรื่อง)──► ② เนื้อเรื่อง ──(สร้างสตอรี่บอร์ด)──► ③ สตอรี่บอร์ด ──(สร้างวิดีโอ)
                    │                                │                                  │
                    └─ (ไม่บังคับ) สร้างภาพปกเรื่อง      └─ แก้ฉาก / เขียนฉากใหม่              └─ แก้ shot / สร้างภาพใหม่ / เพิ่ม-ลบ shot / เลือกภาพปก
```

| ขั้น UI | สิ่งที่ผู้ใช้ทำ | Endpoint หลัก |
|---|---|---|
| หน้าแรก | ลิสต์โปรเจกต์, สร้าง, ลบ | `GET/POST /projects`, `DELETE /projects/:id` |
| ① ไอเดีย | กรอกไอเดีย ตัวละคร รูปแบบ อารมณ์ จำนวนฉาก โมเดล | `PUT /projects/:id/idea` |
| ① ไอเดีย | สร้างภาพปก (ไม่บังคับ) | `POST /projects/:id/cover` |
| ① → ② | สร้างเนื้อเรื่อง | `POST /projects/:id/story` |
| ② เนื้อเรื่อง | แก้ชื่อ/เนื้อเรื่อง/จำนวน shot, เขียนฉากใหม่ | `PATCH /scenes/:sceneId`, `POST /scenes/:sceneId/rewrite` |
| ② → ③ | เลือกโมเดลภาพ/วิดีโอ แล้วสร้างสตอรี่บอร์ด | `POST /projects/:id/storyboard` |
| ③ สตอรี่บอร์ด | แก้คำบรรยาย/มุมกล้อง, สร้างภาพใหม่, เพิ่ม/ลบ shot | `PATCH/DELETE /shots/:shotId`, `POST /shots/:shotId/regenerate`, `POST /scenes/:sceneId/shots` |
| ③ | สร้างวิดีโอทั้งเรื่อง / รายช็อต | `POST /projects/:id/video` (ภาพสตอรี่บอร์ดเป็นเฟรมแรก) |

หน้าเว็บอ่านสถานะทั้งหมดจาก `GET /projects/:id` เพียง endpoint เดียว แล้ว poll ระหว่างที่มีงานค้าง (ดู §9)

---

## 2. ข้อตกลงทั่วไป

- **Base path:** `/open/v1/director`, เรียกผ่าน Next proxy `/api/proxy/director/...`
- **Auth:** JWT เดิม (cookie `ezgen_session` → proxy ใส่ `Authorization: Bearer`) โปรเจกต์เป็นของผู้ใช้คนเดียว ถ้าเป็นโปรเจกต์ของคนอื่นให้ตอบ `404 PROJECT_NOT_FOUND` ไม่ใช่ 403
- **Envelope:** สำเร็จ `{ "data": ..., "requestId": "..." }` / ผิดพลาด `{ code, message, details?, requestId }`
- **ID:** UUID ของแอปทั้งหมด ห้ามส่ง provider task ID หรือ storage key ออกมา
- **เวลา:** ISO 8601 UTC
- **URL ของไฟล์:** signed URL มี `expiresAt` คู่กันเสมอ ถ้าหมดอายุ หน้าเว็บจะเรียก `GET /projects/:id` ใหม่ (ไม่มี endpoint refresh แยก)

### 2.1 Revision (optimistic concurrency)
- `Project.revision` เป็นจำนวนเต็มที่ **เพิ่มขึ้นทุกครั้งที่ผู้ใช้แก้ข้อมูล** (idea, scene, shot, settings)
- งานที่ระบบทำเอง (worker อัปเดต progress/ผลลัพธ์) **ไม่เพิ่ม revision** เพื่อให้ผู้ใช้แก้ข้อความระหว่างที่ภาพกำลังสร้างได้โดยไม่ชน
- คำขอที่แก้ข้อมูลต้องส่ง `revision` ที่อ่านล่าสุดมาด้วย ถ้าไม่ตรงให้ตอบ `409 REVISION_CONFLICT` พร้อม `details.currentRevision` หน้าเว็บจะโหลดใหม่แล้วให้ผู้ใช้ลองอีกครั้ง

### 2.2 Idempotency
- ทุก POST ที่ **เสียพอยต์หรือสร้างงาน AI** ต้องมี `idempotencyKey: UUID`
- ส่ง key เดิมพร้อม payload เดิม ให้คืนผลเดิม (ไม่สร้างงานซ้ำ) ส่ง key เดิมแต่ payload ต่าง ให้ตอบ `409 IDEMPOTENCY_CONFLICT`
- หน้าเว็บเก็บ key ที่ค้างไว้ใน `sessionStorage` เพื่อส่งซ้ำได้เมื่อเน็ตหลุด (แนวเดียวกับ Composer)

### 2.3 พอยต์
- ราคาต่อหน่วยมาจาก `GET /capabilities` และ `Project.estimates` ใช้แสดงก่อนกดสร้าง
- การตัดพอยต์จริงเกิดฝั่งเซิร์ฟเวอร์ตอนรับงาน ถ้าพอยต์ไม่พอให้ตอบ `402 INSUFFICIENT_POINTS` พร้อม `details.required` และ `details.balance`
- งานที่ล้มเหลวโดยไม่ได้ผลลัพธ์ต้องคืนพอยต์ และบันทึก `pointsRefunded` ไว้ใน shot/cover/scene

---

## 3. Capabilities

### `GET /capabilities`

ใช้สร้างตัวเลือกทั้งหมดใน modal สร้างโปรเจกต์และขั้นไอเดีย **หน้าเว็บห้าม hard-code รายการโมเดลหรือราคา**

```ts
interface DirectorCapabilities {
  enabled: boolean;
  videoEnabled: boolean;                     // enabled และตั้งค่าบริการวิดีโอแล้ว
  unavailableReason: string | null;          // ข้อความภาษาไทยที่แสดงผู้ใช้ได้ ระบุสิ่งที่ยังไม่ได้ตั้งค่า
  aspectRatios: Array<{ value: "16:9" | "9:16" | "1:1" | "4:5"; label: string; hint: string }>;
  resolutions: Array<{ value: "720p" | "1080p" | "4k"; label: string; hint: string; available: boolean }>;
  styles: Array<{
    value: string;                           // "cinematic" | "anime" | "3d" | "watercolor" | "clay" | "realistic" | ...
    label: string; hint: string;
    sampleImageUrl: string;                  // ภาพตัวอย่างประจำสไตล์ (static asset, ไม่หมดอายุ) — "" เมื่อยังไม่ได้ตั้ง DIRECTOR_SAMPLE_BASE_URL
  }>;
  genres: Array<{ value: string; label: string; hint: string }>;
  moods: string[];                           // ป้ายภาษาไทย เช่น "อบอุ่น", "ลึกลับ"
  models: {
    story: ModelOption[];                    // โมเดลเขียนเนื้อเรื่อง
    image: ModelOption[];                    // โมเดลสร้างภาพ (ภาพปก + ภาพ shot)
    video: ModelOption[];                    // seedance-2-0-fast | seedance-2-5 | seedance-2-0 (ไม่ขึ้นกับ ARK_DEFAULT_MODEL); id violet-* เดิมถูกแปลงให้อัตโนมัติ
  };
  limits: {
    ideaMaxLength: 1000;
    nameMaxLength: 80;
    minScenes: 1; maxScenes: 10;
    minShotsPerScene: 1; maxShotsPerScene: 6;   // ตอนสร้างสตอรี่บอร์ด
    maxShotsPerSceneAfterEdit: 8;               // เพิ่ม shot เองภายหลังได้ถึง 8
    maxTotalShots: 40;
    maxCast: 4; maxMoods: 3;
    sceneSummaryMaxLength: 1200; shotDescriptionMaxLength: 600;
  };
  cameras: string[];                         // มุมกล้องที่ระบบรองรับ: "มุมกว้าง", "โคลสอัพ", ...
}

interface ModelOption {
  id: string;                                // id ภายในแอป ไม่ใช่ชื่อ provider
  name: string;                              // ชื่อที่แสดงผู้ใช้
  note: string;                              // จุดเด่นสั้น ๆ
  speedHint: string;                         // "~10 วิ/ภาพ"
  pointsPerUnit: number;                     // story: ต่อเรื่อง · image: ต่อภาพ · video: ต่อ shot
  supportsCharacterReference: boolean;       // image/video เท่านั้น
  default?: boolean;
  aspectRatios?: string[];                   // video เท่านั้น: สัดส่วนที่โมเดลรองรับ (4:5 ไม่มีโมเดลวิดีโอรองรับ)
}
```

---

## 4. Data model

```ts
type Step = "idea" | "plot" | "storyboard";
type WorkStatus = "idle" | "queued" | "processing" | "succeeded" | "failed" | "needs_attention";

interface DirectorProject {
  id: string;
  revision: number;
  name: string;
  aspectRatio: "16:9" | "9:16" | "1:1" | "4:5";
  style: string;
  resolution: "720p" | "1080p" | "4k";
  step: Step;                                // ขั้นที่ผู้ใช้เปิดล่าสุด (UI ใช้กำหนดหน้าที่เปิด)
  idea: Idea;
  cover: Cover | null;                       // null = ใช้ภาพตัวอย่างสไตล์ + รูปตัวละครจริง
  story: StoryState;
  storyboard: StoryboardState;
  scenes: Scene[];                           // เรียงตาม index
  estimates: Estimates;
  coverImageUrl: string | null;              // ปกสำหรับหน้าลิสต์: cover > shot ที่เลือกเป็นปก > shot แรกที่เสร็จ > null
  coverImageExpiresAt: string | null;
  createdAt: string;
  updatedAt: string;
}

interface Idea {
  prompt: string;                            // 0–1000
  genre: string;                             // จาก capabilities.genres
  moods: string[];                           // 0–3
  sceneCount: number;                        // 1–10
  characterAssetIds: string[];               // 0–4, asset type=character ของผู้ใช้
  storyModelId: string;
}

interface Cover {
  id: string;
  status: Exclude<WorkStatus, "idle">;
  progress: number;                          // 0–100 จาก provider/worker จริง ห้ามเดาจากเวลา
  imageUrl: string | null;
  expiresAt: string | null;
  source: "generated" | "shot";              // สร้างเอง หรือเลือกจาก shot
  shotId: string | null;                     // เมื่อ source = "shot"
  imageModelId: string | null;
  failureCode: string | null;
  failureMessage: string | null;             // ภาษาไทย แสดงผู้ใช้ได้
  canRetry: boolean;
  pointsCharged: number | null;
}

interface StoryState {
  status: WorkStatus;                        // idle = ยังไม่เคยสร้าง
  phase: "analyzing" | "writing" | "reviewing" | null;   // ตรงกับขั้นย่อยใน banner ของ UI
  logline: string | null;                    // เรื่องย่อ
  storyModelId: string | null;               // snapshot ตอนสร้าง
  completedScenes: number;
  totalScenes: number;
  estimatedSecondsRemaining: number | null;  // null = UI ซ่อนข้อความ "เหลือประมาณ"
  failureCode: string | null; failureMessage: string | null; canRetry: boolean;
}

interface StoryboardState {
  status: WorkStatus;
  imageModelId: string;                      // ค่าที่ผู้ใช้เลือก (แก้ได้จนกดสร้าง)
  videoModelId: string;
  completedImages: number;
  totalShots: number;
  estimatedSecondsRemaining: number | null;
  styleReference: "cover" | "style";         // ภาพปกถูกใช้เป็นภาพอ้างอิงโทนหรือไม่ (ดู §6)
}

interface Scene {
  id: string;
  index: number;                             // เริ่มที่ 1
  status: "pending" | "writing" | "ready" | "failed";
  title: string;
  summary: string;
  mood: string | null;
  characterAssetIds: string[];               // subset ของ idea.characterAssetIds
  shotCount: number;                         // จำนวน shot ที่จะสร้าง (ผู้ใช้ปรับได้ 1–6)
  shots: Shot[];                             // ว่างจนกว่าจะสร้างสตอรี่บอร์ด
  failureCode: string | null; failureMessage: string | null; canRetry: boolean;
}

interface Shot {
  id: string;
  sceneId: string;
  index: number;                             // เริ่มที่ 1 ภายในฉาก
  description: string;                       // คำบรรยายภาพที่ผู้ใช้เห็นและแก้ได้
  camera: string;                            // จาก capabilities.cameras
  characterAssetIds: string[];
  image: {
    status: "pending" | "queued" | "processing" | "succeeded" | "failed" | "needs_attention";
    progress: number;                        // 0–100
    url: string | null;
    expiresAt: string | null;
    stale: boolean;                          // true = คำบรรยาย/มุมกล้องถูกแก้หลังได้ภาพนี้ → UI แสดงปุ่ม "สร้างภาพใหม่"
    failureCode: string | null; failureMessage: string | null; canRetry: boolean;
  };
  video: null | {                            // null = ยังไม่เคยสั่งสร้างวิดีโอ shot นี้
    status: "queued" | "processing" | "succeeded" | "failed" | "needs_attention";
    progress: number;                        // 0–100 (ประมาณจากเวลาที่ผ่านไประหว่างรอ provider)
    url: string | null;                      // signed URL ของไฟล์ mp4 ใน storage ของแอป
    expiresAt: string | null;
    stale: boolean;                          // ภาพ/คำบรรยาย/มุมกล้องเปลี่ยนหลังได้วิดีโอนี้
    modelId: string; durationSeconds: number;
    failureCode: string | null; failureMessage: string | null; canRetry: boolean;
    pointsCharged: number; pointsRefunded: number;
  };
  pointsCharged: number | null;
}

interface Estimates {                        // พอยต์ ณ ค่าปัจจุบัน ใช้แสดงในแผงซ้าย
  story: number;                             // storyModel.pointsPerUnit
  cover: number;                             // imageModel.pointsPerUnit
  storyboard: number;                        // sum(scene.shotCount) × imageModel.pointsPerUnit
  video: number;                             // totalShots × videoModel.pointsPerUnit
}
```

### 4.1 รายการโปรเจกต์ (เบา)
`GET /projects` คืน `DirectorProjectSummary` เพื่อไม่ต้องส่ง scene/shot ทั้งหมด

```ts
interface DirectorProjectSummary {
  id: string; name: string; aspectRatio: string; style: string; resolution: string; step: Step;
  storyStatus: WorkStatus; storyboardStatus: WorkStatus;
  sceneCount: number; totalShots: number; completedImages: number;
  coverImageUrl: string | null; coverImageExpiresAt: string | null;
  updatedAt: string;
}
```

---

## 5. Projects

| Method | Path | Body | Response | หมายเหตุ |
|---|---|---|---|---|
| GET | `/projects?page=1&limit=20&search=` | – | `{ items: DirectorProjectSummary[], total, page, limit }` | เรียงตาม `updatedAt desc`, limit ≤ 50, search ≤ 80 ตัวอักษร (ค้นจากชื่อ) |
| POST | `/projects` | `{ name, aspectRatio, style, resolution, idempotencyKey }` | `DirectorProject` | modal "สร้างโปรเจกต์ใหม่" เปิดที่ขั้น `idea` ค่าเริ่มต้น `sceneCount: 3`, genre และโมเดลที่เป็น `default` |
| GET | `/projects/:id` | – | `DirectorProject` | ใช้ poll |
| PATCH | `/projects/:id` | `{ revision, name?, aspectRatio?, style?, resolution?, step? }` | `DirectorProject` | ดูกฎด้านล่าง |
| DELETE | `/projects/:id` | – | `204` | soft delete ยกเลิกงานที่ค้าง และไม่ลบ asset ตัวละครในคลัง |

กฎของ `PATCH /projects/:id`
- `name` และ `step` แก้ได้ตลอด แต่ `step` ไปได้เฉพาะขั้นที่ถึงแล้ว: `plot` ต้องมี `story.status ≠ idle` และ `storyboard` ต้องมี `storyboard.status ≠ idle`
- `step` **ไม่เพิ่ม revision** เพราะเป็นแค่สถานะการนำทาง
- `aspectRatio`, `style`, `resolution` แก้ได้เฉพาะตอน `storyboard.status = idle` ถ้าแก้หลังจากนั้นให้ตอบ `409 DIRECTOR_STATE_CONFLICT` โดยข้อความแนะนำให้สร้างสตอรี่บอร์ดใหม่
- ถ้าแก้ `style` ระหว่างที่มี cover แบบ `generated` อยู่ ให้ตั้ง `cover = null` (ภาพปกไม่ตรงสไตล์แล้ว)

---

## 6. ① ไอเดีย และภาพปกเรื่อง

### `PUT /projects/:id/idea`
```ts
// body
{ revision: number; prompt: string; genre: string; moods: string[]; sceneCount: number;
  characterAssetIds: string[]; storyModelId: string }
// → DirectorProject
```
- หน้าเว็บส่งแบบ debounce 600 ms ขณะพิมพ์ (autosave) และส่งทันทีเมื่อกดตัวเลือก
- validate: ความยาวตาม `limits`, genre/mood/model ต้องอยู่ใน capabilities, ตัวละครต้องเป็นของผู้ใช้และ `assetType = character` ถ้าไม่ผ่านให้ตอบ `400 VALIDATION_ERROR` พร้อม `details[].field`
- แก้ idea หลังมีเนื้อเรื่องแล้วได้ **แต่จะไม่เขียนเนื้อเรื่องใหม่ให้อัตโนมัติ** ผู้ใช้ต้องกด "เขียนเนื้อเรื่องใหม่" เอง (§7)

### ภาพบนโปสเตอร์ขั้นไอเดีย (กฎการแสดงผล)
1. `cover.status = succeeded`: ใช้ `cover.imageUrl` และติดป้าย "ภาพปกจาก AI"
2. `cover.status ∈ queued|processing`: แสดงภาพตัวอย่างสไตล์ ทับด้วย skeleton และแถบ `cover.progress`
3. `cover = null` หรือ `failed`: ใช้ `capabilities.styles[style].sampleImageUrl` และวางรูปตัวละครจริงเป็นวงกลม (`Asset.mediaUrl` ของ `idea.characterAssetIds`) ติดป้าย "ภาพตัวอย่างสไตล์…" กรณี `failed` ให้แสดง `cover.failureMessage` ใต้โปสเตอร์

### `POST /projects/:id/cover` — สร้างภาพปกด้วย AI
```ts
// body
{ revision: number; idempotencyKey: string; imageModelId?: string }   // ไม่ส่ง = ใช้ storyboard.imageModelId
// → DirectorProject  (cover.status = "queued")
```
- เงื่อนไข: `idea.prompt.trim().length ≥ 10` และต้องไม่มี cover ที่กำลังสร้างอยู่ (ถ้ามีให้ตอบ `409 COVER_IN_PROGRESS`)
- ตัดพอยต์ `estimates.cover`
- prompt ที่ส่ง provider: ประกอบฝั่งเซิร์ฟเวอร์จาก `idea.prompt`, genre, moods, style และ aspectRatio ของโปรเจกต์ ใช้ภาพอ้างอิงตัวละครได้ 1 ภาพตามข้อจำกัด adapter เดิม (ตัวละครตัวแรก) ส่วนตัวที่เหลือใส่เป็นข้อความบรรยาย
- ใช้ image job pipeline เดิม (`jobs` table/worker) แต่**ไม่สร้าง asset ในคลัง**
- การสร้างใหม่จะแทนที่ปกเดิม ภาพเก่าเก็บไว้ใน storage ได้ แต่ไม่ต้องส่งออกมา

### `PUT /projects/:id/cover` — ใช้ภาพ shot เป็นปก
```ts
{ revision: number; shotId: string }      // shot ต้อง image.status = succeeded → cover.source = "shot"
```

### `DELETE /projects/:id/cover` — กลับไปใช้ภาพตัวอย่างสไตล์
`{ revision }` (ส่งใน body หรือ `?revision=`) → `DirectorProject` (`cover = null`) ปุ่มนี้คือ "ใช้ภาพตัวอย่างสไตล์" ใน UI และไม่คืนพอยต์

### ใช้ภาพปกเป็นภาพอ้างอิงโทน
ตอนสร้างสตอรี่บอร์ด ถ้า `cover.status = succeeded` และ `source = generated` ให้ส่งภาพปกเป็น **style/tone reference** ร่วมกับภาพอ้างอิงตัวละคร (ถ้า adapter รองรับหลายภาพ) แล้วตั้ง `storyboard.styleReference = "cover"` ถ้ารองรับภาพเดียว ให้ภาพตัวละครมาก่อน แล้วสรุปโทนสีจากภาพปกเป็นข้อความแทน
UI แสดงแถว "อ้างอิงโทนภาพ: ภาพปกเรื่อง / สไตล์…" ในแผงซ้ายของขั้นเนื้อเรื่อง

### ตัวละคร (แผงขวาขั้นไอเดีย)
**ไม่มี endpoint ใหม่** ใช้ของ Character Studio เดิม
- ลิสต์: `GET /open/v1/assets?type=character&page=1&limit=50`
- สร้างตัวละครใหม่จาก modal: `POST /open/v1/image-jobs` `{ category: "character", prompt, aspectRatio: "3:4", designFormat: "full_body", idempotencyKey }` แล้ว poll `GET /jobs/:jobId` → เมื่อสำเร็จเรียก `POST /open/v1/assets` เพื่อบันทึก จากนั้นเพิ่ม id ลง `idea.characterAssetIds`

---

## 7. ① → ② เนื้อเรื่อง

### `POST /projects/:id/story` — สร้าง/เขียนเนื้อเรื่องใหม่ทั้งหมด
```ts
{ revision: number; idempotencyKey: string; replace: boolean }
// → DirectorProject  (story.status = "queued", scenes = sceneCount รายการ status "pending", step = "plot")
```
- ต้องผ่าน validation ของ idea (prompt ≥ 10, genre, moods ≥ 1)
- ถ้ามีเนื้อเรื่องอยู่แล้วต้องส่ง `replace: true` ไม่งั้นตอบ `409 STORY_EXISTS` (UI ถามยืนยัน "เนื้อเรื่องและสตอรี่บอร์ดเดิมจะถูกแทนที่")
- `replace` ล้าง scenes/shots เดิม และตั้ง `storyboard.status = idle` แต่ cover ยังอยู่
- **สร้าง scene placeholder ครบตามจำนวนทันที** (status `pending`) เพื่อให้ UI แสดง skeleton ได้ครบตั้งแต่แรก แล้ว worker เติมทีละฉาก: `pending → writing → ready`
- `story.phase`: `analyzing` (ยังไม่มีฉากไหนเริ่ม) → `writing` → `reviewing` (ตรวจความต่อเนื่อง) → `null` เมื่อ `succeeded`
- เขียน `story.logline` ให้เสร็จในเฟส `analyzing`
- ตัดพอยต์ `estimates.story` ครั้งเดียวต่อคำขอ

### `PATCH /projects/:id/scenes/:sceneId`
```ts
{ revision: number; title?: string; summary?: string; shotCount?: number }
// → DirectorProject
```
- แก้ได้เมื่อ `scene.status = ready` และ `storyboard.status ∉ queued|processing`
- `shotCount` มีผลกับการสร้างสตอรี่บอร์ดครั้งถัดไปเท่านั้น ไม่เพิ่มหรือลบ shot ที่มีอยู่แล้ว

### `POST /projects/:id/scenes/:sceneId/rewrite` — ให้ AI เขียนฉากนี้ใหม่
```ts
{ revision: number; idempotencyKey: string; instruction?: string }   // instruction (≤ 300) สำหรับอนาคต UI ยังไม่ส่ง
// → DirectorProject  (scene.status = "writing")
```
- เขียนใหม่ได้ทีละฉาก ถ้ามีฉากอื่นกำลังเขียนอยู่ให้ตอบ `409 SCENE_BUSY`
- ใช้ฉากก่อนและหลังเป็นบริบทให้เรื่องต่อเนื่อง และคง `shotCount` เดิม
- ตัดพอยต์ตาม `models.story.pointsPerUnit` ÷ `sceneCount` (ปัดขึ้น) หรือให้ฟรีตามนโยบาย **ต้องตัดสินใจ (§13)**

---

## 8. ② → ③ สตอรี่บอร์ด

### `PATCH /projects/:id/storyboard-settings`
```ts
{ revision: number; imageModelId?: string; videoModelId?: string }   // dropdown ในแผงซ้าย · → DirectorProject
```
แก้ไม่ได้ระหว่าง `storyboard.status ∈ queued|processing`

### `POST /projects/:id/storyboard` — สร้างภาพทุก shot
```ts
{ revision: number; idempotencyKey: string; replace: boolean }
// → DirectorProject  (storyboard.status = "queued", step = "storyboard")
```
- ต้องมี `story.status = succeeded` และทุกฉาก `ready`
- ถ้ามี shot อยู่แล้วต้องส่ง `replace: true` ไม่งั้นตอบ `409 STORYBOARD_EXISTS`
- ขั้นตอนฝั่งเซิร์ฟเวอร์:
  1. แตกแต่ละฉากเป็น shot ตาม `scene.shotCount` ด้วยโมเดล story (description, camera, characterAssetIds)
  2. **สร้าง Shot record ครบทุกตัวทันที** (`image.status = "pending"`) เพื่อให้ UI แสดงกรอบ skeleton ครบ
  3. สร้างภาพทีละ shot ตามลำดับฉากและ shot โดย concurrency ต่อโปรเจกต์ไม่เกิน 2 (ปรับได้จาก config) และอัปเดต `image.progress`
- ตัดพอยต์ `estimates.storyboard` ตอนรับงาน ถ้ามีภาพล้มเหลวให้คืนเป็นรายภาพ
- shot ที่เสร็จแล้วยังอยู่แม้ shot อื่นล้มเหลว และ `storyboard.status = succeeded` เมื่อทุก shot `succeeded` ถ้ามีบางตัว `failed` ให้เป็น `needs_attention`

### Shot

| Method | Path | Body | ผล |
|---|---|---|---|
| PATCH | `/projects/:id/shots/:shotId` | `{ revision, description?, camera? }` | แก้ข้อความทันทีโดยไม่สร้างภาพใหม่ ถ้าภาพสำเร็จแล้วให้ตั้ง `image.stale = true` ต้องแก้ได้แม้ภาพของ shot นั้นกำลังสร้าง (ภาพที่กำลังทำใช้ข้อความเก่า แล้วได้ `stale = true` เมื่อเสร็จ) |
| POST | `/projects/:id/shots/:shotId/regenerate` | `{ revision, idempotencyKey }` | สร้างภาพใหม่จาก description/camera ปัจจุบัน → `image.status = queued`, `stale = false` ตัดพอยต์ 1 ภาพ ภาพเดิมยังแสดงจนภาพใหม่เสร็จ (UI แสดง overlay progress) |
| POST | `/projects/:id/scenes/:sceneId/shots` | `{ revision, idempotencyKey, afterShotId?: string }` | AI เขียน shot ต่อจากเรื่องเดิม แล้วสร้างภาพ สร้าง record ทันทีด้วย `image.status = pending` ไม่เกิน `maxShotsPerSceneAfterEdit` (`409 SHOT_LIMIT`) |
| DELETE | `/projects/:id/shots/:shotId?revision=` | – | `DirectorProject` ฉากต้องเหลืออย่างน้อย 1 shot (`409 LAST_SHOT`) แล้วจัด index ใหม่ ถ้า shot นั้นเป็นปกให้ `cover = null` |
| POST | `/projects/:id/shots/:shotId/retry` | `{ revision, idempotencyKey }` | เฉพาะ `canRetry = true` (`failed` / `needs_attention`) |

### วิดีโอ
- `POST /projects/:id/video` `{ revision, idempotencyKey, shotIds?: string[] }` → `DirectorProject` (revision ไม่เปลี่ยน)
- ไม่ส่ง `shotIds` = ทุก shot ที่ยังไม่มีวิดีโอสำเร็จหรือวิดีโอ `stale` (ไม่มีเลย → `409 VIDEO_UP_TO_DATE`); ส่ง `shotIds` = สร้างใหม่เฉพาะ shot ที่ระบุ
- เงื่อนไข: ภาพของทุก shot ที่จะใช้ต้อง `succeeded` (`409 STORYBOARD_INCOMPLETE`), shot ไม่มีงานค้าง (`409 SHOT_BUSY`), โมเดล `storyboard.videoModelId` รองรับสัดส่วนของโปรเจกต์ (`422 MODEL_NOT_SUPPORTED`)
- ตัดพอยต์ `videoModel.pointsPerUnit` ต่อ shot; provider ล้มเหลวแบบชัดเจนคืนพอยต์และ `canRetry = true`
- ความยาว 5 วินาทีต่อ shot (ปรับตามขั้นต่ำ/สูงสุดของโมเดล), ความละเอียดตามโปรเจกต์แต่ไม่เกินที่โมเดลรองรับ, ภาพ shot เป็น first frame
- ระหว่างมีวิดีโอค้าง: สร้างเนื้อเรื่องใหม่/สตอรี่บอร์ดใหม่/เขียนฉากใหม่ → `409 DIRECTOR_STATE_CONFLICT`, ลบ/สร้างภาพ shot นั้นใหม่ → `409 SHOT_BUSY`
- แก้ description/camera หรือได้ภาพใหม่หลังวิดีโอสำเร็จ → `video.stale = true`

---

## 9. Progress และการ poll

- หน้าเว็บ poll `GET /projects/:id` **ทุก 2 วินาที** ระหว่างที่มีอย่างน้อยหนึ่งข้อ: `story.status`, `storyboard.status` หรือ `cover.status` เป็น `queued|processing`, หรือมี `scene.status = writing`, หรือมี `shot.image.status ∈ queued|processing`
- หยุด poll เมื่อแท็บถูกซ่อน และ poll ทันทีเมื่อกลับมา (ใช้ `useRefreshPolling` เดิม)
- **progress ต้องมาจากความคืบหน้าจริง** (provider progress หรือจำนวนงานย่อยที่เสร็จ) ห้ามคำนวณจากเวลาที่ผ่านไป
- `estimatedSecondsRemaining` เป็น `null` ได้ ถ้าเป็น null UI จะไม่แสดง "เหลือประมาณ…"
- ตอบ `ETag` (`W/"<revision>-<workVersion>"`) แล้วรับ `If-None-Match` เพื่อคืน `304` เมื่อไม่มีอะไรเปลี่ยน
- ทางเลือกในอนาคต: `GET /projects/:id/events` (SSE) ส่ง event `project.updated` ที่มี patch แบบเดียวกัน หน้าเว็บยัง fallback เป็น poll ได้

### การจับคู่กับ UI

| UI | ค่าที่ใช้ |
|---|---|
| แถบ progress ใต้ปุ่ม "เนื้อเรื่อง" บนแถบบน | `story.completedScenes / story.totalScenes` |
| banner ขั้นเนื้อเรื่อง: วิเคราะห์ / เขียนฉาก x/y / ตรวจความต่อเนื่อง | `story.phase`, `completedScenes`, `totalScenes` |
| skeleton ฉาก "รอคิว" / "กำลังเขียนฉากที่ n…" | `scene.status = pending / writing` |
| แถบ progress ใต้ปุ่ม "สตอรี่บอร์ด" + "กำลังสร้างภาพ x/y" | `storyboard.completedImages / totalShots` |
| กรอบ shot "รอคิว" / "กำลังสร้างภาพ n%" | `shot.image.status`, `shot.image.progress` |
| โปสเตอร์ขั้นไอเดีย | กฎใน §6 |
| ปกในหน้าลิสต์ | `coverImageUrl` |

---

## 10. State machine และสิทธิ์ทำ action

| Action | ทำได้เมื่อ | ถ้าทำไม่ได้ |
|---|---|---|
| แก้ idea | ทุกสถานะ (ยกเว้น `story.status ∈ queued|processing`) | 409 `DIRECTOR_STATE_CONFLICT` |
| สร้างภาพปก | prompt ≥ 10 และไม่มี cover ค้าง | 400 / 409 `COVER_IN_PROGRESS` |
| สร้างเนื้อเรื่อง | idea ผ่าน validation, ไม่มีงาน story/storyboard ค้าง | 400 / 409 |
| แก้ฉาก / เขียนฉากใหม่ | `scene.status = ready` และ storyboard ไม่ได้กำลังสร้าง | 409 `SCENE_BUSY` / `DIRECTOR_STATE_CONFLICT` |
| สร้างสตอรี่บอร์ด | `story.status = succeeded` และทุกฉาก ready | 409 |
| แก้ shot (ข้อความ) | shot มีอยู่ | – |
| สร้างภาพ shot ใหม่ / retry | ภาพของ shot นั้นไม่ได้กำลังสร้าง | 409 `SHOT_BUSY` |
| ลบ shot | ฉากเหลือ > 1 shot | 409 `LAST_SHOT` |

Worker: ใช้กฎเดียวกับ Story Flow คือ lease ในฐานข้อมูล, ตั้ง `submitting` ก่อนเรียก provider, ถ้าการสร้างกำกวมหรือถูกขัดจังหวะให้เป็น `needs_attention` และ**ไม่ส่งซ้ำอัตโนมัติ**

---

## 11. Error codes

| HTTP | code | เมื่อ | UI ทำอะไร |
|---|---|---|---|
| 400 | `VALIDATION_ERROR` | field ผิด (`details[]: {field, reason, message}`) | ไฮไลต์ช่องตาม `field` |
| 402 | `INSUFFICIENT_POINTS` | พอยต์ไม่พอ (`details.required`, `details.balance`) | แสดงข้อความ + ลิงก์เติมพอยต์ |
| 404 | `PROJECT_NOT_FOUND` / `SCENE_NOT_FOUND` / `SHOT_NOT_FOUND` | ไม่มีหรือไม่ใช่ของผู้ใช้ | หน้า "ไม่พบโปรเจกต์" |
| 409 | `REVISION_CONFLICT` | revision ไม่ตรง (`details.currentRevision`) | โหลดใหม่ + toast "มีการแก้ไขจากอีกแท็บ" |
| 409 | `IDEMPOTENCY_CONFLICT` | key ซ้ำแต่ payload ต่าง | สร้าง key ใหม่แล้วถามผู้ใช้ |
| 409 | `STORY_EXISTS` / `STORYBOARD_EXISTS` | ไม่ได้ส่ง `replace: true` | dialog ยืนยันแทนที่ |
| 409 | `COVER_IN_PROGRESS` / `SCENE_BUSY` / `SHOT_BUSY` | มีงานค้างอยู่ | ปิดปุ่มไว้จนงานเสร็จ |
| 409 | `SHOT_LIMIT` / `LAST_SHOT` | เกินหรือต่ำกว่าขีดจำกัด | ปิดปุ่ม |
| 409 | `DIRECTOR_STATE_CONFLICT` | action ไม่ตรงกับ state | โหลดใหม่ |
| 429 | `RATE_LIMITED` | เกินโควตาต่อนาที/งานพร้อมกัน | แสดง `Retry-After` |
| 409 | `VIDEO_UP_TO_DATE` / `STORYBOARD_INCOMPLETE` | ไม่มี shot ต้องสร้างวิดีโอ / ภาพยังไม่ครบ | ปิดปุ่ม |
| 422 | `MODEL_NOT_SUPPORTED` | โมเดลวิดีโอไม่รองรับสัดส่วนโปรเจกต์ | ให้เลือกโมเดลอื่น |
| 503 | `DIRECTOR_UNAVAILABLE` | ยังไม่ได้ตั้งค่า provider/โมเดล | แสดง `capabilities.unavailableReason` |
| 503 | `DIRECTOR_VIDEO_UNAVAILABLE` | ยังไม่ได้ตั้งค่าบริการวิดีโอ (`ARK_API_KEY`) | ปิดปุ่มตาม `capabilities.videoEnabled` |
| 500 | `INTERNAL_ERROR` | อื่น ๆ | ข้อความทั่วไป + requestId |

ทุกข้อความ (`message`, `failureMessage`) เป็นภาษาไทยที่แสดงผู้ใช้ได้ ห้ามมีชื่อ provider, task ID หรือ stack trace

---

## 12. ตัวอย่าง

**สร้างโปรเจกต์**
```http
POST /open/v1/director/projects
{ "name": "ตำนานอัศวินแห่งรุ่งอรุณ", "aspectRatio": "16:9", "style": "cinematic", "resolution": "1080p",
  "idempotencyKey": "5b0f…" }
```

**สร้างภาพปก → poll**
```http
POST /open/v1/director/projects/9c1e…/cover
{ "revision": 4, "idempotencyKey": "a71d…" }

→ 200 { "data": { "id": "9c1e…", "revision": 4,
    "cover": { "id": "c2…", "status": "queued", "progress": 0, "imageUrl": null, "source": "generated", … },
    "estimates": { "story": 4, "cover": 3, "storyboard": 27, "video": 180 }, … } }
```

**ระหว่างสร้างเนื้อเรื่อง (ส่วนหนึ่งของ `GET /projects/:id`)**
```json
{
  "revision": 5,
  "step": "plot",
  "story": { "status": "processing", "phase": "writing", "logline": "อัศวินผู้โดดเดี่ยว…",
             "completedScenes": 1, "totalScenes": 3, "estimatedSecondsRemaining": 12 },
  "scenes": [
    { "id": "s1", "index": 1, "status": "ready", "title": "หมู่บ้านที่เวทมนตร์จางหาย", "summary": "…", "shotCount": 3, "shots": [] },
    { "id": "s2", "index": 2, "status": "writing", "title": "", "summary": "", "shotCount": 3, "shots": [] },
    { "id": "s3", "index": 3, "status": "pending", "title": "", "summary": "", "shotCount": 3, "shots": [] }
  ]
}
```

---

## 13. คำถามที่ต้องตัดสินใจก่อนลง API

1. **ราคาการเขียนฉากใหม่รายฉาก**: ตัดพอยต์ตามสัดส่วน หรือให้ฟรี N ครั้งต่อโปรเจกต์?
2. **Resolution 4K**: โมเดลวิดีโอปัจจุบันรองรับหรือไม่ ถ้าไม่ให้ `available: false` ใน capabilities (UI จะปิดตัวเลือก)
3. **ภาพอ้างอิงหลายภาพ**: adapter ภาพรองรับภาพอ้างอิงมากกว่า 1 ภาพเมื่อไหร่ (ภาพตัวละคร + ภาพปกเป็นโทน)
4. **ภาพตัวอย่างสไตล์**: ทีมออกแบบเตรียมภาพ static สไตล์ละ 1–2 ภาพ (16:9 และ 9:16) แล้วเก็บไว้ที่ไหน (CDN/public bucket)
5. **ย้ายข้อมูล Story Flow เดิม**: ต้องย้ายโปรเจกต์ Story Flow v1 มาเป็น Director หรือไม่ (ข้อเสนอ: ไม่ย้าย และยังเปิดหน้าเดิมได้จาก URL)
6. **ชื่อโมเดลที่แสดงผู้ใช้**: ใช้ชื่อแบรนด์ provider หรือชื่อภายใน (เช่น "มาตรฐาน / ละเอียด / เร็ว")

---

## 14. สิ่งที่หน้าเว็บต้องเปลี่ยนตอนต่อ API

| ไฟล์ | ตอนนี้ (mock) | หลังต่อ API |
|---|---|---|
| `director-data.ts` | ค่าคงที่ STYLES/GENRES/MODELS, `localStorage` | ย้ายไป `src/lib/director-api.ts` + อ่านจาก `GET /capabilities` |
| `director-project.tsx` | interval จำลองทุก 650 ms | `useRefreshPolling` → `GET /projects/:id` ทุก 2 วินาที |
| `shot-art.tsx` | ภาพวาดจากโค้ด | `<Image unoptimized src={url}>` + `onError` → poll ใหม่ (URL หมดอายุ) |
| `step-idea.tsx` | ตัวละครจำลอง, ภาพปกจำลอง | `GET /assets?type=character`, `POST /cover`, `sampleImageUrl` |
| ทุก step | `update()` แก้ state ตรง ๆ | เรียก PATCH ตาม §5–8 แบบ optimistic แล้วใช้ผลจาก response แทน |

---

## 15. Implementation decisions — 2026-11-04

This addendum records phase-one decisions where the draft leaves a choice or
requires behavior the current adapters cannot provide. The original draft above
is preserved for traceability. Runtime verification and deployment readiness are
recorded separately in the workspace task report.

- Director uses independent project, job, idempotency receipt, point-account and
  point-ledger tables. Existing Story Flow projects are not migrated. Worker
  completion merges into the latest locked document and fences results by job
  lease and generation, so user text edits can survive an in-flight image call.
- Existing users have no general points account in this codebase. Director
  accounts start at **zero** and require explicit operator funding; no free or
  inferred balance is granted. A separate payment/top-up integration is outside
  this task. Charge and refund entries are persisted atomically with job state.
- A scene rewrite costs `ceil(storyPoints / sceneCount)`. Cover, scene, shot and
  story responses add `pointsRefunded` to satisfy §2.3, although the draft
  interfaces omitted those fields. Prices come from server configuration and
  the same values drive capabilities, estimates and charges.
- Shot placeholders are created when storyboard work is accepted, before AI
  planning fills descriptions and cameras. This makes immediate skeletons
  possible without waiting for a provider round trip. Progress comes from
  actual state changes; the synchronous image adapter provides no intermediate
  provider percentage, so an image remains at 0 until completion, then 100.
- The current image adapter accepts one reference image. Character reference
  takes priority. A cover can be the image reference when no character is used.
  Do not infer that cover colors were analyzed when only configured style text
  was supplied; `styleReference` must describe the reference actually used.
- 4K remains unavailable. Story/image model names are derived from the configured
  model (e.g. "Gemini 3.5 Flash", "Seedream 5.0"). Video choices are fixed
  Seedance 2.0 Fast / 2.5 / 2.0 entries resolved directly from provider configs,
  so the registry's `ARK_DEFAULT_MODEL` override cannot collapse them into one
  model. Stored `violet-*` IDs are mapped to their Seedance 2.x equivalents.
- Planner output is decoded tolerantly (code fences, extra fields, renumbered shot
  IDs, near-miss camera/mood labels are normalized). If shot planning still fails,
  empty shot descriptions fall back to the scene title and summary so storyboard
  images are generated. Failures store a specific code/message and are logged as
  `director: job=… kind=… code=… cause=…` without prompts or credentials.
- Video jobs submit once, then move to `waiting` with the provider handle held
  only in `director_jobs.output`. Later claims poll that handle and never
  resubmit. The provider task ID never appears in a response. Finished clips are
  copied to application storage under `director/<user>/<project>/<shot>/<job>.mp4`.
- Static style samples must be supplied by the operator/design team. Do not
  fabricate a CDN URL or claim sample files were uploaded by this implementation.
- The follow-up Web integration uses `/api/proxy/director/*`, server DTOs and
  dynamic capabilities. The shared Next proxy now supports PATCH, DELETE
  bodies and ETag/If-None-Match/Retry-After forwarding with bodyless204/304
  responses. DELETE cover also supports the revision query parameter. Existing
  browser-only demo projects are not migrated. Integration verification is
  tracked separately in `docs/engineering/tasks/director-web-20261104.md` at the
  workspace root; deployment prerequisites below remain operator actions.

### Operator configuration

Apply migration `029_director.sql` through the normal deployment process before
enabling the API; this implementation task does not apply production migrations.
Story planning uses `DIRECTOR_STORY_PROVIDER` (`gemini` with `GEMINI_API_KEY` /
`GEMINI_MODEL`, or `modelark` with `ARK_STORY_ANALYSIS_MODEL`); when blank, Gemini is
chosen if no ModelArk text model is set but a Gemini key is. Images use
`ARK_IMAGE_MODEL`; shot videos use `ARK_API_KEY` and the video registry. `DIRECTOR_STORY_POINTS` defaults to 4,
`DIRECTOR_IMAGE_POINTS` to 3 and `DIRECTOR_VIDEO_POINTS` to 20. These are application
point prices, not a provider-cost quotation. `DIRECTOR_SAMPLE_BASE_URL` names the
public static sample directory (optional: blank shows a placeholder); supply `cinematic.jpg`, `anime.jpg`, `3d.jpg`,
`watercolor.jpg`, `clay.jpg` and `realistic.jpg` there. The task does not publish
those design assets.

Fund a user's `director_point_accounts` record through an authorized operator
process before paid generation. The server does not initialize a positive
balance or expose a top-up endpoint. Existing project reads and editing must
remain usable when generation prerequisites are missing; AI requests return
`503 DIRECTOR_UNAVAILABLE` until configured.

The phase-one worker submits one provider operation at a time, which keeps
per-project concurrency below two. There is no configurable parallel image
submission in this version. A failed scene rewrite with `canRetry: true` can be
submitted to the rewrite endpoint again, in addition to the draft's `ready`
scene precondition; otherwise a recoverable failed scene would have no retry
route. Ambiguous provider outcomes keep `canRetry: false` and require operator
reconciliation rather than automatic resubmission.

Project deletion cancels and refunds queued jobs that have not been submitted,
and fences publication of in-flight results. The current synchronous provider
adapter has no remote cancellation operation: a call already sent may finish,
but its result is not published to a deleted project. An expired lease on a
deleted project's submitted job is reconciled without resubmitting or assuming
a refund for an uncertain provider outcome.

When a media URL expires or an image load fails, refresh the project with an
**unconditional GET: omit `If-None-Match`**. The revision/workVersion ETag tracks
project data, not the lifetime of signed URLs; sending the unchanged ETag may
correctly return 304 and cannot supply fresh URLs. Normal progress polling may
still use conditional GETs.
