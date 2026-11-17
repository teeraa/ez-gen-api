package facade_open_api

// filmCameraRules is the camera grammar given to the film planner. Clips are generated one shot at a
// time and continuation shots start on the previous clip's last frame, so a camera move that eases
// in and out inside every shot reads as "pan, stop, new move". The rules keep moves short, single
// and consistent across a continuation chain.
const filmCameraRules = `- Camera ("camera" field): one shot size, one angle and exactly ONE movement, chosen from: static, slow push in, slow pull out, pan left/right, tilt up/down, tracking (follow a subject), handheld, crane up, slow orbit. Never combine movements or describe a change of movement inside a shot.
- Establishing: a scene opens on at most ONE wide establishing shot of 5-10 seconds with a single slow move; never spend two or more shots touring the same place.
- Camera continuity inside a scene: a shot that continues from the previous shot keeps the same movement direction at the same constant speed, or holds static; it never reverses, stops and restarts, or switches to a different movement. Prefer static or a subtle drift for dialogue and emotional beats; save tracking or handheld for action.
- Pans and orbits are slow and short (they read as a tour if they last long); after one moving shot, prefer a static or subtle shot.`
