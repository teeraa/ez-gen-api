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

- **Base path:** `/api/v1/director`, เรียกผ่าน Next proxy `/api/proxy/director/...`
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
  seriesId: string | null; episodeNumber: number | null; episodeTitle: string | null; // ดู §16
}
```

`DirectorProject` มีสามฟิลด์ `seriesId`, `episodeNumber`, `episodeTitle` เหมือนกัน (มีเสมอ เป็น `null` สำหรับโปรเจกต์เดี่ยว)

---

## 5. Projects

| Method | Path | Body | Response | หมายเหตุ |
|---|---|---|---|---|
| GET | `/projects?page=1&limit=20&search=` | – | `{ items: DirectorProjectSummary[], total, page, limit }` | เรียงตาม `updatedAt desc`, limit ≤ 50, search ≤ 80 ตัวอักษร (ค้นจากชื่อ) **คืนเฉพาะโปรเจกต์เดี่ยว (`seriesId = null`)** ตอนของซีรีส์ดูผ่าน `GET /series/:id` |
| POST | `/projects` | `{ name, aspectRatio, style, resolution, idempotencyKey }` | `DirectorProject` | modal "สร้างโปรเจกต์ใหม่" เปิดที่ขั้น `idea` ค่าเริ่มต้น `sceneCount: 3`, genre และโมเดลที่เป็น `default` |
| GET | `/projects/:id` | – | `DirectorProject` | ใช้ poll |
| PATCH | `/projects/:id` | `{ revision, name?, aspectRatio?, style?, resolution?, step?, episodeTitle? }` | `DirectorProject` | ดูกฎด้านล่าง |
| DELETE | `/projects/:id` | – | `204` | soft delete ยกเลิกงานที่ค้าง และไม่ลบ asset ตัวละครในคลัง |

กฎของ `PATCH /projects/:id`
- `name` และ `step` แก้ได้ตลอด แต่ `step` ไปได้เฉพาะขั้นที่ถึงแล้ว: `plot` ต้องมี `story.status ≠ idle` และ `storyboard` ต้องมี `storyboard.status ≠ idle`
- `step` **ไม่เพิ่ม revision** เพราะเป็นแค่สถานะการนำทาง
- `aspectRatio`, `style`, `resolution` แก้ได้เฉพาะตอน `storyboard.status = idle` ถ้าแก้หลังจากนั้นให้ตอบ `409 DIRECTOR_STATE_CONFLICT` โดยข้อความแนะนำให้สร้างสตอรี่บอร์ดใหม่
- ถ้าแก้ `style` ระหว่างที่มี cover แบบ `generated` อยู่ ให้ตั้ง `cover = null` (ภาพปกไม่ตรงสไตล์แล้ว)
- `episodeTitle` (0–120 ตัวอักษร, ค่าว่าง/ช่องว่างล้วน = `null`) ใช้ได้เฉพาะตอนของซีรีส์ และเพิ่ม revision; ส่งให้โปรเจกต์เดี่ยวได้ `400 VALIDATION_ERROR` field `episodeTitle` การแก้ `episodeTitle` ไม่เปลี่ยน `name`

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
- ลิสต์: `GET /api/v1/assets?type=character&page=1&limit=50`
- สร้างตัวละครใหม่จาก modal: `POST /api/v1/image-jobs` `{ category: "character", prompt, aspectRatio: "3:4", designFormat: "full_body", idempotencyKey }` แล้ว poll `GET /jobs/:jobId` → เมื่อสำเร็จเรียก `POST /api/v1/assets` เพื่อบันทึก จากนั้นเพิ่ม id ลง `idea.characterAssetIds`

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
- `POST /projects/:id/video` `{ revision, idempotencyKey, shotIds?: string[], draft?: boolean, finalize?: boolean }` → `DirectorProject` (revision ไม่เปลี่ยน)
- Frame continuity source: the first shot of each scene uses its own storyboard image. Each subsequent shot uses the **actual final frame of its preceding completed video in the same scene**, copied into durable storage. The next storyboard image is no longer an end-frame target. `video.usesLastFrame` means deriving the clip from that preceding final frame. First shots and models without video-extension support send this image as the provider's `first_frame`, with no additional reference images: first-frame and omni-reference modes cannot be mixed. Source clip/frame identity is rechecked before submission and on completion.

- Video extension for normal (non-filmed-synthetic) styles: first shots retain `first_frame`; subsequent shots on Seedance 2.0 Fast, 2.0 and 2.5 use ONLY the preceding current video's last **3 seconds** as a single `reference_video`. The local FFmpeg tail is MP4, 30 FPS, short edge 720, with audio preserved when present. No cumulative chain/full-history reference or image roles are mixed into extension requests. Other unsupported models retain first-frame behavior. Missing local continuation support fails explicitly before provider submission.
- Extension prompts explicitly say “Extend Video1 forward” and request ONLY 5 seconds of new footage after its final frame, preserving motion/camera/lighting without replaying the input. Seedance 2.5 extension uses `ratio: adaptive`; 2.0/fast use the saved project ratio. Draft2.5 remains480p; finalization inherits its accepted draft without preparing or sending a new tail. App points remain unchanged; upstream video-reference usage may increase provider cost.
- Provider guidance describes extension output as usually new footage, not an unconditional concatenation contract. We never blindly subtract the reference length. New output must decode and measure the requested duration within0.1 seconds; unexpected combined input+output becomes `needs_attention` with `VIDEO_EXTENSION_OUTPUT_INVALID`. Duration validation cannot detect semantically repeated footage of the same length, and local mocked requests do not guarantee live visual continuity.
- Extension preparation and its owned tail key/source clip/frame are checkpointed through preparing/ready/submitting phases. Local preparation retries use the same source; source changes block paid submission. A durable submitting marker with uncertain outcome is never automatically resubmitted. Accepted tasks checkpoint the provider handle before releasing the lease, then checkpoint owned output before local duration/frame validation. Draft/refinement retain extension lineage and output validation. Existing jobs/clips keep their original generation inputs.
- v10 (2026-11-08): for `realistic` and `cinematic`, every shot (first or later) is prepared from its OWN storyboard frame as a characters-only stylized 3D pose-casting image on a plain light-gray background (`director-pose-casting-3d-v1`); the environment is supplied as text (`Scene environment:`) in the video prompt, the reference is used only for identity, outfit and pose (`director-pose-casting-v1`), and the user's base block is omitted in this mode. These shots no longer depend on the previous clip (no VIDEO_DEPENDENCY_NOT_READY, `usesLastFrame=false`). Earlier history follows.
- 2026-11-09: for `realistic` and `cinematic`, only the **first shot of each scene** goes through the Seedream pose-casting → Seedance path above. Every later shot in the same scene continues from its predecessor exactly like the other styles (clip extension on models with `SupportsVideoExtension`, otherwise the actual final frame as `first_frame`), reports `video.usesLastFrame: true`, and requires the predecessor video to have succeeded. A new scene starts again from its own storyboard frame and camera.
- For `realistic` and `cinematic` (v2–v9), a private synthetic 3D scene is generated from that selected source before video submission. Later shots derive it from the actual preceding video frame, never from their independent storyboard still. Director image preparation uses its own `CompileDirectorFilmedScene`. v8 (`director-filmed-scene-cg-v1`, near-photoreal CG faces) was rejected by Seedance reference-image moderation in live testing, so v9 (`director-filmed-scene-stylized-v2`, user-directed 2026-11-08) keeps faces clearly stylized and non-photographic (clean painted skin, no pores) while hair silhouette, fabric structure and scene lighting stay real; realism is produced only by the video prompt; the shared `CompilePipelineStylizedScene` used elsewhere is unchanged; video compilation uses `pipeline-filmed-human-v8`. The video receives this one synthetic reference only, with cast descriptions as text; raw cast images and raw photoreal frames are excluded. The visible storyboard remains unchanged. Re-rendering preserves intended composition/identity rather than guaranteeing an exact frame match or live-provider acceptance.
- Director video rendering now opts into a typed filmed look: `realistic` selects natural photographic exposure/color and available light; `cinematic` selects motivated photographic key/fill/practical lighting with restrained film color and plausible optical depth. Both replace the synthetic reference's CG materials with subtle skin texture/pores at normal shot scale, attentive moist eyes/blinks, individual hair, real fabric, grounded weight and restrained expressions. Preserve fictional broad identity, age, build, clothing, composition and scene time of day; do not invent a real-person identity or apply blanket beauty retouching, bokeh or extreme color grading. The original synthetic image prepass is unchanged. The video-only controls request natural-scale skin pores/uneven roughness/localized restrained highlights and individual strands/flyaways while preserving identity and composition; no new image treatment is requested.
- Compiled video text (controls, selected scene content, negative constraints and dialogue) is no longer silently cut at2000 characters. Its application safety cap is16000 Unicode code points, not tokens/words or a verified provider hard limit. Provider guidance separately recommends concise prompts (up to500 Chinese characters or1000 English words). Over-cap Director jobs fail explicitly with `VIDEO_PROMPT_TOO_LONG` before video submission; filmed preparation validates before image generation and refunds the reservation on this definitive failure. Original image prompts, raw public input limits and intentional character-summary caps remain unchanged. Prepared/submitted jobs retain their frozen text.
- Look controls apply only to the opted-in filmed `PreserveScene` reference path and precede the creative brief inside the compiled-video application safety budget of16000 Unicode code points. Empty/unknown look values and other pipelines retain their previous behavior; explicit first frames do not opt into reinterpretation. New Director requests use `director-scene-3d-v9/director-filmed-scene-stylized-v2/.../director-user-filmed-template-v1/director-filmed-layout-v4/director-filmed-neutral-v1`; the lead sentence tells the model the reference is stylized CG concept art for design/layout only and to cast each character as a real person with the same apparent age, build, hairstyle and outfit. Art-style words in shot/cast/setting text (e.g. 3D, CGI, Pixar, anime, cartoon, stylized, illustration, การ์ตูน, สามมิติ, อนิเมะ) are removed for these two styles before both the image prepass and the video prompt. The compiled video prompt starts with one lead sentence (live-action output; the reference is a CG previsualization for identity/layout/composition only), followed by the product owner's full video template verbatim, laid out as (1) base block with the reference index adapted to `Image 1`, (2) ONLY the selected `REALISTIC` or `CINEMATIC` mode block, (3) a `Shot:` section — scene environment, action, cast descriptions, camera and continuation note, one per line, untrimmed — and (4) `Negative constraints:` with the user's list last, as prompt text (no separate `negative_prompt` field). v6 placed shot context after the negative list without separation; prepared v6 jobs keep their frozen text; the whole compiled prompt is validated against the 16000 code-point application budget before the paid synthetic image call, failing with `VIDEO_PROMPT_TOO_LONG`. Prepared jobs carrying known v2–v8 fingerprints resume their frozen request without regenerating the image or changing prompts; a checkpoint whose synthetic key or request shape cannot be verified stops with `VIDEO_SYNTHETIC_REFERENCE_UNVERIFIED` before any provider call. Already submitted videos are unaffected. Local tests establish prompt construction and selection, not a verified live improvement in photographic realism or provider acceptance.
- Synthetic preparation adds one image-provider operation per newly submitted video job, without a separate application point debit. Source key, image model, style, compiler versions and request are frozen per job; generated bytes are checkpointed before upload. Upload/signing retries reuse that checkpoint. There is no cross-job synthetic cache: a newly requested regeneration can incur another image-provider operation. Ambiguous image/video submission results become `needs_attention` and are never automatically resubmitted. Definitive preparation failure refunds the video reservation under the existing refund lifecycle; invalid preparation never falls back to raw images.
- For first-frame fallback, other styles send exactly one strict `first_frame` image; selected cast descriptions remain text, without Image 2 labels or raw character references. Draft generation uses synthetic preparation for the two filmed styles; draft finalization inherits the accepted draft and does not regenerate an intermediate or resubmit its prompt/frames. Existing clips/already submitted jobs retain their original inputs; generate new clips to exercise this correction. Local request tests prove conditioning semantics, not guaranteed visual smoothness from a generative provider.
- Batch jobs follow storyboard order, regardless of `shotIds` order; each dependent clip waits for its predecessor's video and durable frame before provider submission. New scenes start with their own shot image.
- Selecting a later shot without a valid, current predecessor video/frame (including legacy clips without saved frames) returns `409 VIDEO_DEPENDENCY_NOT_READY`; regenerate the preceding clip first. If a batch predecessor fails, dependent jobs fail before provider submission and refund their reserved points.
- Refinement cannot change its inherited draft frames: finalizing a dependent shot requires its saved draft source to match the current completed predecessor. Finalizing both predecessor and dependent together returns `409 VIDEO_DEPENDENCY_NOT_READY` before charging; finalize the predecessor, regenerate dependent drafts against that result, then finalize them. Legacy dependent drafts without source lineage require regeneration. Stale clips block new exports (`409 VIDEO_INCOMPLETE`) and mark existing exports stale.
- Every video request asks for `return_last_frame`. Missing, invalid or unavailable provider frames fall back to decoding the **actual final frame of the generated video locally with FFmpeg**. The uploaded owned-video key and source lineage are checkpointed before frame persistence; retries read that owned video and never resubmit paid generation. Production requires a processor implementing `videoprocessing.LastFrameExtractor` (the existing FFmpeg processor does). Frame persistence is limited to three persisted attempts, then `needs_attention` with `VIDEO_FRAME_RECOVERY_FAILED`; no additional charge/refund occurs.
- On restart/poll, only current, unrefunded video jobs stuck at `VIDEO_LAST_FRAME_MISSING` are automatically claimed for local recovery. The deterministic owned video key is checkpointed atomically with its lease; other attention failures are untouched. No provider task is created or polled. Missing owned media stops after the bounded attempts. Deleted projects/shots and superseded generations are excluded.
- Legacy failure completion cleared provider/source metadata. Recovered later shots are visible as `succeeded` with `stale=true` when their source lineage cannot be proven; subsequent clips in that scene become stale and exports require review/regeneration. Recovered legacy drafts without a provider draft handle cannot be finalized. Current checkpointed jobs retain their actual source lineage.


- `draft: true` (เฉพาะโมเดลที่ `supportsDraft`, ตอนนี้ Seedance 2.5): 480p ราคา `draftPointsPerUnit` → `video.draft = true`; `finalize: true` ทำฉบับเต็ม 1080p จาก draft ที่ยังไม่หมดอายุ (`video.canFinalize`, ไม่ส่ง `shotIds` = ทุก shot ที่ทำได้) ราคาเต็ม; โมเดลไม่รองรับ → `422 DRAFT_NOT_SUPPORTED`, ไม่มี draft ที่ใช้ได้ → `409 DRAFT_NOT_AVAILABLE`
- ไม่ส่ง `shotIds` = ทุก shot ที่ยังไม่มีวิดีโอสำเร็จหรือวิดีโอ `stale` (ไม่มีเลย → `409 VIDEO_UP_TO_DATE`); ส่ง `shotIds` = สร้างใหม่เฉพาะ shot ที่ระบุ
- เงื่อนไข: ภาพของทุก shot ที่จะใช้ต้อง `succeeded` (`409 STORYBOARD_INCOMPLETE`), shot ไม่มีงานค้าง (`409 SHOT_BUSY`), โมเดล `storyboard.videoModelId` รองรับสัดส่วนของโปรเจกต์ (`422 MODEL_NOT_SUPPORTED`)
- ตัดพอยต์ `videoModel.pointsPerUnit` ต่อ shot; provider ล้มเหลวแบบชัดเจนคืนพอยต์และ `canRetry = true`
- ความยาว 5 วินาทีต่อ shot (ปรับตามขั้นต่ำ/สูงสุดของโมเดล), ความละเอียดตามโปรเจกต์แต่ไม่เกินที่โมเดลรองรับ, first frame ตามลำดับต่อเนื่องข้างต้น
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
| 409 | `SERIES_BUSY` | ลบซีรีส์ขณะมีตอนที่มีงาน queued/submitting/waiting | แจ้งให้รองานเสร็จก่อนลบ |
| 409 | `EPISODE_LIMIT` | ซีรีส์มีตอนที่ 999 แล้ว | ปิดปุ่มสร้างตอน |
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
POST /api/v1/director/projects
{ "name": "ตำนานอัศวินแห่งรุ่งอรุณ", "aspectRatio": "16:9", "style": "cinematic", "resolution": "1080p",
  "idempotencyKey": "5b0f…" }
```

**สร้างภาพปก → poll**
```http
POST /api/v1/director/projects/9c1e…/cover
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
- Prompt expansion: the story step also returns an English `visualBible` and a
  per-scene `setting`; shot planning returns an English `prompt` per shot (kept
  private with a hash of the Thai description/camera it was written for; a user
  edit falls back to the Thai text). Image prompts combine camera term, shot
  action, every shot character bound to reference Image N (all shot characters,
  up to 4, are sent as references), scene setting, visual bible and a style
  descriptor. Video prompts add camera motion, cast continuity and setting.
- Character looks: with the Gemini planner, each character reference image is described once
  per project (outfit, colors, hair, accessories) and cached privately by storage key. The same
  text is given to the planner and to every image/video prompt with a wardrobe lock; shot
  prompts must not restyle named characters. Without Gemini the library description is used.
- Director video uses a ModelArk client without AI-usage recording: `ai_usage.job_id` references
  `video_generation_jobs`, which Director jobs are not. Errors raised before a provider request is
  sent (`modelark.ErrNotSubmitted`) are refunded and retryable, not "uncertain".
- The Gemini planner retries 429/5xx/transport failures up to 3 attempts (honoring Retry-After,
  max 30s) and logs the HTTP status and provider message on final failure.
- Seedance screens references for real-looking faces (`InputImageSensitiveContentDetected` →
  `PROVIDER_IMAGE_REJECTED`). Rejection happens at submission, so Director retries once with the
  storyboard frames only (no character sheets) and then with the first frame only. Terminal task
  failures show the allowlisted provider reason. `DIRECTOR_VIDEO_HUMAN_FACE_MODE` optionally sends
  `human_face_mode` (SDK field; live effect not yet verified).
- `POST /projects/:id/export` `{ revision, idempotencyKey }` → `DirectorProject` with
  `export: { status, progress, url, expiresAt, stale, clipCount, failure… }`. Requires a finished
  clip for every shot (`409 VIDEO_INCOMPLETE`), one export at a time (`409 EXPORT_IN_PROGRESS`),
  no points charged. The worker merges the clip keys frozen at request time in story order with
  FFmpeg and stores `director/<user>/<project>/export/<job>.mp4`; `stale` turns true when any clip
  changes afterwards. `503 DIRECTOR_EXPORT_UNAVAILABLE` when no processor is configured.
- `GET /director/points` → `{ balance }`: the caller's Director point balance (0 when never
  funded, `Cache-Control: private, no-store`). The web navbar shows this real balance.
- Scenes expose an editable `setting` (location/lighting, ≤400 chars, `PATCH /scenes/:id`
  `{ setting }`); it leads every image prompt of the scene and is restated at the end, so
  shots whose description omits the place keep the same background. Editing it marks the
  scene's images (and videos) stale.
- Shot cast is narrowed to characters named in the shot text, in mention order, before the
  single reference image is chosen; a close-up of one character no longer borrows another
  character's reference. With no names mentioned the planned cast is kept.
- Image prompts open and close with the selected art style ("overrides the style of any
  reference image"), and references are limited to identity (face, body, hair, outfit), so a
  photoreal character sheet no longer forces every shot into its own rendering and tone.
  Up to `DIRECTOR_IMAGE_MAX_REFERENCES` (default 4) named characters are sent as references.
- Image prompts state the scene environment before the cast and tell the model that
  references define identity only, because character references are usually shot on
  plain backgrounds. Duplicate library character names get a letter suffix.
- While an image request runs, `image.progress`/`cover.progress` is an estimate from
  elapsed time (5→95, updated every 2s); providers report no intermediate progress.
- Each character reference URL is checked (ranged GET) before submission; unreachable
  ones are dropped and logged. A rejected request (invalid input, 404, unknown provider
  error; never content-policy or account errors) is retried with one reference, then
  none. Server logs carry the redacted raw provider code/message as `detail=`.
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

---

## 16. Series & episodes — 2026-11-08

A series (UI label "โปรเจกต์") groups episodes. Each episode **is** an ordinary
`director_projects` row, so every `/projects/:id/...` endpoint (idea, cover, story,
storyboard, shots, video, export, PATCH, DELETE) works unchanged on an episode.
Migration: `migrations/030_director_series.sql` (`director_series`,
`director_series_receipts`, `director_projects.series_id/episode_number/episode_title`,
unique `(series_id, episode_number)` among live rows). All routes are under
`/api/v1/director`, JWT, user-scoped, envelope `{data, requestId}`.

```ts
interface DirectorSeries {
  id: string; revision: number; name: string;
  aspectRatio: string; style: string; resolution: string;
  characterAssetIds: string[];          // never null
  episodeCount: number;                 // live episodes
  coverImageUrl: string | null;         // cover of the lowest-numbered episode that has one
                                        // (its succeeded cover, else its first succeeded shot image)
  coverImageExpiresAt: string | null;
  createdAt: string; updatedAt: string;
}
interface DirectorSeriesDetail extends DirectorSeries {
  episodes: DirectorProjectSummary[];   // ordered by episodeNumber asc, never null
}
```

`GET /series` computes `episodeCount` and the cover key in SQL and signs one URL per
series; it never decodes episode documents. `GET /series/:id` decodes each episode to
build its summary but signs only each summary's cover URL (no shot, video or export URLs).

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/series` | `{ name, mode?, aspectRatio, style, resolution, characterAssetIds?, idempotencyKey }` | `201 DirectorSeries` |
| GET | `/series?page=1&limit=20&mode=director` | – | `200 { items: DirectorSeries[], total, page, limit }` newest `updatedAt` first, `limit` 1–50, only series of `mode` (default `director`) |
| GET | `/series/:id` | – | `200 DirectorSeriesDetail` (`Cache-Control: private, no-cache`, no ETag) |
| PATCH | `/series/:id` | `{ revision, name?, aspectRatio?, style?, resolution?, characterAssetIds? }` | `200 DirectorSeries` |
| DELETE | `/series/:id` | – | `204` |
| POST | `/series/:id/episodes` | `{ title?, idempotencyKey }` | `201 DirectorProject` |

Validation (`400 VALIDATION_ERROR`, `details[0].field`): `name` 1–120 runes (not blank);
`aspectRatio`/`style`/`resolution` use the capability enums (unavailable resolutions such
as `4k` rejected); `characterAssetIds` ≤ 4 unique UUIDs of the caller's `character` assets
with an image; `title` 0–120 runes; `idempotencyKey` UUID; unknown body fields rejected.

Rules
- **Ownership**: another user's series id, a malformed id, or a deleted series → `404 PROJECT_NOT_FOUND` (same code as projects).
- **Idempotency** (POST `/series`, POST `/series/:id/episodes`): same key + same body returns the same resource with `201` — for `/series` its current state, for `/series/:id/episodes` the episode document as it was at creation (identical to `POST /projects` replay; reload with `GET /projects/:id` for the latest); same key + different body → `409 IDEMPOTENCY_CONFLICT`; replaying after deletion → `404`. Series keys and project/episode keys are separate namespaces.
- **PATCH** requires `revision`; a stale one → `409 REVISION_CONFLICT` with `details.currentRevision`. A PATCH that changes any field increments `revision` and `updatedAt`; a PATCH with only `revision` changes neither. Changing defaults never rewrites existing episodes.
- **Episode creation**: `episodeNumber = max(live episode numbers) + 1` (so deleting the last episode frees its number), assigned under the owner's Director lock and the series row lock; concurrent creates get distinct numbers. The episode starts at `step: "idea"` with `aspectRatio`/`style`/`resolution` copied from the series and `idea.characterAssetIds` copied from the series cast (assets that no longer exist or are not characters are dropped). `episodeTitle` = trimmed `title` or `null`. `name` = the title, or `"<series name> ตอนที่ N"`; because project names are limited to 80 runes, the title is clipped to 80 runes and the series-name part is shortened so the suffix ` ตอนที่ N` is always kept. Above 999 episodes → `409 EPISODE_LIMIT`.
- **DELETE**: if any live episode has a Director job in `queued`, `submitting` or `waiting` → `409 SERIES_BUSY` and nothing changes; otherwise the series and all its episodes are soft-deleted in one transaction.
- **Mode** (2026-11-12, `migrations/032_director_series_mode.sql`): `DirectorSeries.mode` is `"director"` (ผู้กำกับ) or `"film"` (สร้างหนัง). `POST /series` defaults it to `director`; any other value → `400 VALIDATION_ERROR` field `mode`. It is fixed at creation: `PATCH` with a different `mode` → `400` field `mode` (the same value is accepted and ignored). `GET /series` lists only the requested mode (unknown → `400` field `mode`). Detail, episodes, jobs, points and deletion are shared and ignore mode. Existing rows are `director`.
- **Legacy**: projects created before series keep `seriesId: null`, appear only in `GET /projects`, and behave exactly as before.

Story context: when an episode's story (`POST /projects/:id/story`) runs, the planner input
gains a `series` object — a fixed English note that this is one episode, the series name
(≤120 runes), this episode's number and title (≤120 runes), and up to 5 nearest earlier live
episodes (lower `episodeNumber`) as `{number, title, logline}` in ascending order (title falls
back to the episode name; logline ≤300 runes, from the stored story). This adds at most
≈2.5k runes. If the lookup fails the story runs without it. Standalone projects' planner
input is byte-identical to the pre-series format (golden test
`TestDirectorStandaloneStoryPromptByteIdentical`). Scene rewrite, shot planning, image and
video prompts are unchanged.

## Props registry, face and object integrity, defaults — 2026-11-09

- New projects default to `seedance-2-5` (first entry of the video model list); existing projects keep their stored model.
- Story generation requires at least one reference character (`characterAssetIds`, validation field `characterAssetIds`).
- The story planner now also returns `props` (≤6, each `{name, spec}`; spec ≤180 chars of real-world physical structure). They are stored privately in the project document, reset when the story is replaced, and passed to the shot planner.
- Image, first-frame video, extension and realistic/cinematic pose-casting prompts restate the structure of up to two props named in the shot, plus fixed object-integrity (one handle/hilt, scabbard is a separate sheath, no duplicated or bent parts) and face-integrity rules.
- Planner guidance limits shots to two clearly visible faces, requires explicit left/right placement, and stages face-to-face contact over the shoulder or as hand/weapon close-ups.


## Experiment A outcome — faceless casting default (2026-11-09)

- Realistic/cinematic now default to the A1 "faceless" recipe: the Seedream prepass keeps pose, costume and props photographically but blurs every face; faces come from the character text. Project setting `filmedRecipe` (`storyboard-settings`) can opt into `pose3d` (previous stylized-3D casting), `clay` or `textonly`; `GET /projects/:id/experiments` lists clips per recipe.
- Later shots in a realistic/cinematic scene never use clip extension (the provider rejects realistic people in a reference video with HTTP 400). They start on the previous clip's actual last frame as `first_frame` for a seamless cut (task `rawFrame`). If the provider rejects that frame (`PROVIDER_IMAGE_REJECTED`, at submit or as a task failure), the same job falls back once, at no extra charge, to faceless re-casting of that frame (reference-image mode, weaker continuity), keeping source-clip lineage checks.
- Planned realistic/cinematic close-ups with two or more characters are reframed as over-the-shoulder shots, because outputs with several realistic faces in close-up are rejected (`OutputVideoSensitiveContentDetected`).
- `InputVideoSensitiveContentDetected` maps to `PROVIDER_VIDEO_REJECTED`; extension submit failures now log provider code and param.
