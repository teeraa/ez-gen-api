BEGIN;
-- ภาพยนตร์ skill: a fixed (locked-off) camera for every shot, no push in / pull out / zoom / moves.
-- Rhythm comes from cutting between fixed set-ups. The video prompt locks any shot whose camera
-- says static (filmLockedCamera), so the model does not drift the framing on its own.
INSERT INTO skills (id, user_id, name, description, icon, steps, inputs, output, directive, sort_order) VALUES
 ('c0000000-0000-4000-8000-000000000010', NULL, 'กล้องนิ่ง ไม่ดัน ไม่ซูม', 'ทุกช็อตตั้งกล้องนิ่งบนขาตั้ง ไม่ดันเข้า ไม่ถอยออก ไม่ซูม ไม่แพน สร้างจังหวะด้วยการตัดสลับมุมกล้องแทน', 'camera',
  '["ตั้งกล้องนิ่งทุกช็อต ไม่มีการเคลื่อนกล้อง","อยากเน้นอารมณ์ให้ตัดไปช็อตที่แคบลง แทนการดันกล้องเข้า","เปลี่ยนมุมด้วยการตัด: กว้าง กลาง ใกล้ ข้ามไหล่ ด้านข้าง มุมสูง/ต่ำ"]', '["character"]', 'film',
  'Locked-off camera. Every shot is filmed from a fixed tripod position: the "camera" field starts with "Static locked-off" and names only the shot size, angle and lens; never a push in, pull out, zoom, dolly, pan, tilt, tracking, crane, orbit or handheld move, and never a change of framing inside a shot. This overrides any camera move asked for by another chosen skill: where it would push in for emphasis, cut to a tighter static shot instead; where it would track or pan to follow someone, let them move through a still frame, or cut to the next angle. Build rhythm and variety only by cutting between fixed set-ups: a wide establishing frame, mediums, close-ups and inserts, over-the-shoulder and reverse angles, profiles, slightly high or low angles. Frame with room for the action so people can enter, cross and leave the frame instead of the camera following them. Keep the 180-degree line and matching eyelines across cuts. A shot that continues from the previous one stays static as well.', 19)
ON CONFLICT (id) DO NOTHING;
COMMIT;
