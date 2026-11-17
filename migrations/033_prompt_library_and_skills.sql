BEGIN;

-- user_id NULL marks a built-in row shared by every user; built-ins are read-only through the API.
CREATE TABLE prompt_templates (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid REFERENCES users(id),
  title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 60),
  category text NOT NULL CHECK (category IN ('video','image','product','character','camera')),
  body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX prompt_templates_owner ON prompt_templates(user_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX prompt_templates_builtin ON prompt_templates(sort_order) WHERE user_id IS NULL AND deleted_at IS NULL;

CREATE TABLE prompt_bookmarks (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  prompt_id uuid NOT NULL REFERENCES prompt_templates(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, prompt_id)
);

-- steps/inputs are JSON arrays so a later migration can turn steps into typed objects
-- (kind + params) for the skill runner without a table rewrite.
CREATE TABLE skills (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid REFERENCES users(id),
  forked_from_id uuid REFERENCES skills(id) ON DELETE SET NULL,
  name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
  description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 300),
  icon text NOT NULL DEFAULT 'wand',
  steps jsonb NOT NULL CHECK (jsonb_typeof(steps) = 'array' AND jsonb_array_length(steps) BETWEEN 1 AND 8),
  inputs jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(inputs) = 'array'),
  output text NOT NULL CHECK (output IN ('video_9_16','video_16_9','storyboard','image_set')),
  version integer NOT NULL DEFAULT 1,
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX skills_owner ON skills(user_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX skills_builtin ON skills(sort_order) WHERE user_id IS NULL AND deleted_at IS NULL;

INSERT INTO prompt_templates (id, user_id, title, category, body, sort_order) VALUES
 ('a0000000-0000-4000-8000-000000000001', NULL, 'ช็อตเปิดเรื่องแบบซีเนมาติก', 'video', 'Cinematic establishing shot of {สถานที่} at {ช่วงเวลา}, slow dolly forward, volumetric light, shallow depth of field, 35mm film grain', 1),
 ('a0000000-0000-4000-8000-000000000002', NULL, 'สินค้าบนพื้นหินอ่อน', 'product', 'Studio product photo of {สินค้า} on white marble, soft key light from left, subtle reflection, clean {สีพื้นหลัง} background, high detail', 2),
 ('a0000000-0000-4000-8000-000000000003', NULL, 'ตัวละครเต็มตัว มุมตรง', 'character', 'Full body character sheet of {ตัวละคร}, front view, neutral pose, plain light grey background, consistent proportions, {สไตล์}', 3),
 ('a0000000-0000-4000-8000-000000000004', NULL, 'โดรนบินผ่านภูเขา', 'camera', 'Aerial drone shot gliding over {ภูมิประเทศ}, sunrise haze, smooth forward motion, wide angle, 4K', 4),
 ('a0000000-0000-4000-8000-000000000005', NULL, 'ภาพประกอบสไตล์สีน้ำ', 'image', 'Soft watercolor illustration of {ฉาก}, pastel palette, paper texture, gentle light, storybook feel', 5),
 ('a0000000-0000-4000-8000-000000000006', NULL, 'ซูมเข้าหน้าตัวละคร', 'camera', 'Slow push-in close-up on {ตัวละคร} face, {อารมณ์} expression, rim light, background bokeh', 6),
 ('a0000000-0000-4000-8000-000000000007', NULL, 'สินค้าหมุน 360°', 'video', '{สินค้า} rotating 360 degrees on a turntable, studio lighting, seamless {สีพื้นหลัง} backdrop, loopable', 7),
 ('a0000000-0000-4000-8000-000000000008', NULL, 'เมืองยามค่ำคืนนีออน', 'image', 'Neon-lit {เมือง} street at night after rain, reflections on wet asphalt, cyberpunk mood, wide shot', 8)
ON CONFLICT (id) DO NOTHING;

INSERT INTO skills (id, user_id, name, description, icon, steps, inputs, output, sort_order) VALUES
 ('b0000000-0000-4000-8000-000000000001', NULL, 'โฆษณาสินค้า 15 วินาที', 'เปิดด้วยปัญหา โชว์สินค้า ปิดด้วยคำชวนซื้อ เหมาะกับ TikTok และ Reels', 'megaphone',
  '["วิเคราะห์จุดเด่นสินค้า","เขียนบท 3 ช็อต","สร้างภาพแต่ละช็อต","ใส่เสียงพากย์และเพลง"]', '["product","voice"]', 'video_9_16', 1),
 ('b0000000-0000-4000-8000-000000000002', NULL, 'เรื่องสั้น 5 ฉาก', 'ขยายไอเดียประโยคเดียวเป็นเรื่องสั้น 5 ฉาก คงตัวละครเดิมทุกฉาก', 'clapperboard',
  '["แตกโครงเรื่อง 5 ฉาก","ล็อกตัวละครหลัก","สร้างภาพนิ่งทีละฉาก","แปลงเป็นวิดีโอ"]', '["character","style"]', 'video_16_9', 2),
 ('b0000000-0000-4000-8000-000000000003', NULL, 'รีวิวสินค้าแบบ UGC', 'วิดีโอรีวิวสไตล์คนจริงถือสินค้า พูดกับกล้อง เป็นกันเอง', 'shopping-bag',
  '["กำหนดผู้รีวิว","เขียนสคริปต์รีวิว","สร้างวิดีโอ + ลิปซิงก์"]', '["character","product","voice"]', 'video_9_16', 3),
 ('b0000000-0000-4000-8000-000000000004', NULL, 'สตอรี่บอร์ดจากบท', 'วางบทหรือเรื่องย่อ แล้วได้สตอรี่บอร์ดพร้อมมุมกล้องทุกช็อต', 'file-text',
  '["แบ่งบทเป็นช็อต","กำหนดมุมกล้อง","สร้างภาพสเก็ตช์"]', '["script","style"]', 'storyboard', 4),
 ('b0000000-0000-4000-8000-000000000005', NULL, 'ตัดไฮไลต์อัตโนมัติ', 'เลือกช่วงที่น่าสนใจจากคลิปยาว ใส่ซับและทรานซิชันให้พร้อมโพสต์', 'scissors',
  '["หาช่วงไฮไลต์","ตัดและเรียง","ใส่ข้อความและเพลง"]', '["music"]', 'video_9_16', 5)
ON CONFLICT (id) DO NOTHING;

COMMIT;
