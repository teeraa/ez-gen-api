package facade_open_api

// Film edits by position (ภาพยนตร์ phase 6). Once an episode has shots, every chat edit must say
// where it applies: a target picked in the timeline or "ฉาก N · ช็อต M" in the message (shots are
// numbered again inside each scene). Without one the server asks back at once, without calling
// the assistant and without charging. The assistant answers with a list of operations
// (add / delete / edit / voice) on named positions instead of a whole new shot list; the server
// checks every position and asks back when one does not exist. An edit that loses or outdates a
// paid clip, or a voice change that costs points (re-dub, or a new video for clips whose voice is
// part of the video), waits for the user to confirm a card.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/google/uuid"
)

// filmPlanModeOps marks plan jobs that answer with operations.
const filmPlanModeOps = "ops"

// filmMaxOps bounds one answer; more is treated as a misunderstanding and asked back.
const filmMaxOps = 60

// filmEditNamespace derives the idempotency keys of the jobs a confirmed edit starts.
var filmEditNamespace = uuid.MustParse("6d8f2f0e-6c1a-4f4e-9f57-6a0c9a3f7e21")

// filmPos is a position in the episode: scene and shot inside the scene, both 1-based; Shot 0 is
// the whole scene.
type filmPos struct{ Scene, Shot int }

// filmPosLabel is how the UI and the assistant name a position.
func filmPosLabel(scene, shot int) string {
	if shot <= 0 {
		return fmt.Sprintf("ฉาก %d", scene)
	}
	return fmt.Sprintf("ฉาก %d · ช็อต %d", scene, shot)
}

// filmScenes groups shot indexes into scenes: a scene starts at the first shot and at every cut.
func filmScenes(shots []filmShot) [][]int {
	out := [][]int{}
	for i := range shots {
		if i == 0 || shots[i].Transition != "continue" {
			out = append(out, []int{i})
		} else {
			out[len(out)-1] = append(out[len(out)-1], i)
		}
	}
	return out
}

// filmPosOf finds a shot's position.
func filmPosOf(scenes [][]int, index int) filmPos {
	for s, sc := range scenes {
		for m, k := range sc {
			if k == index {
				return filmPos{s + 1, m + 1}
			}
		}
	}
	return filmPos{}
}

var filmPosPattern = regexp.MustCompile(`(?i)(ฉาก|ซีน|scene|ช็อต|ช็อท|ชอต|ชอท|shot)\s*(?:ที่)?\s*([0-9๐-๙]+)`)

// filmMention is one "ฉาก N" / "ช็อต M" in a message; N is -1 when the number does not fit.
type filmMention struct {
	Scene      bool
	N          int
	Start, End int
}

// filmPositionMentions finds the positions a message names. English words count only as whole
// words ("screenshot 9" is not a shot).
func filmPositionMentions(prompt string) []filmMention {
	out := []filmMention{}
	for _, m := range filmPosPattern.FindAllStringSubmatchIndex(prompt, -1) {
		word := strings.ToLower(prompt[m[2]:m[3]])
		if word == "scene" || word == "shot" {
			if r, _ := utf8.DecodeLastRuneInString(prompt[:m[0]]); m[0] > 0 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
				continue
			}
		}
		n, e := strconv.Atoi(filmThaiDigits(prompt[m[4]:m[5]]))
		if e != nil || n > 1000 {
			n = -1
		}
		out = append(out, filmMention{Scene: word == "ฉาก" || word == "ซีน" || word == "scene", N: n, Start: m[0], End: m[1]})
	}
	return out
}

func filmThaiDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '๐' && r <= '๙' {
			return '0' + (r - '๐')
		}
		return r
	}, s)
}

// filmPromptCheck checks the positions a message names against the episode. It returns "" when
// the message (with the timeline target) says where to edit and every named position exists, or
// the question to ask back.
func filmPromptCheck(prompt string, scenes [][]int, target *filmPos) string {
	mentions := filmPositionMentions(prompt)
	if len(mentions) == 0 && target == nil {
		return "ต้องการแก้ตรงไหนครับ? ระบุเป็น “ฉาก N” หรือ “ฉาก N · ช็อต M” ในข้อความ (เช่น “ฉาก 2 · ช็อต 1 ให้กล้องซูมเข้า”) หรือเลือกฉาก/ช็อตใน timeline ก่อนส่ง · ยังไม่คิดแต้ม"
	}
	current := 0
	if target != nil {
		current = target.Scene
	}
	for k, m := range mentions {
		if m.Scene {
			// One past the last scene is allowed: "add scene N" appends a scene.
			if m.N < 1 || m.N > len(scenes)+1 {
				return fmt.Sprintf("ไม่พบฉาก %s ตอนนี้มี %d ฉาก ช่วยระบุตำแหน่งใหม่อีกครั้ง · ยังไม่คิดแต้ม", filmMentionNumber(m.N), len(scenes))
			}
			current = m.N
			continue
		}
		scene := current
		// Thai often names the shot first: "ช็อต 2 ของฉาก 3".
		if k+1 < len(mentions) && mentions[k+1].Scene && filmShotOfScene(prompt[m.End:mentions[k+1].Start]) {
			scene = mentions[k+1].N
		}
		if scene == 0 {
			if len(scenes) != 1 {
				return fmt.Sprintf("ช็อต %s ของฉากไหนครับ? เลขช็อตนับใหม่ในแต่ละฉาก กรุณาระบุเป็น “ฉาก N · ช็อต %s” · ยังไม่คิดแต้ม", filmMentionNumber(m.N), filmMentionNumber(m.N))
			}
			scene = 1
		}
		if scene < 1 || scene > len(scenes)+1 {
			return fmt.Sprintf("ไม่พบฉาก %s ตอนนี้มี %d ฉาก ช่วยระบุตำแหน่งใหม่อีกครั้ง · ยังไม่คิดแต้ม", filmMentionNumber(scene), len(scenes))
		}
		size := 0
		if scene <= len(scenes) {
			size = len(scenes[scene-1])
		}
		if m.N < 1 || m.N > size+1 {
			return fmt.Sprintf("ไม่พบ ฉาก %d · ช็อต %s (ฉาก %d มี %d ช็อต) ช่วยระบุตำแหน่งใหม่อีกครั้ง · ยังไม่คิดแต้ม", scene, filmMentionNumber(m.N), scene, size)
		}
	}
	return ""
}

// filmShotOfScene tells whether the words between "ช็อต M" and a following "ฉาก N" tie them
// ("ช็อต 2 ของฉาก 3", "ช็อต 2 ในฉาก 3"), as opposed to a list ("ฉาก 1 ช็อต 3, ฉาก 2").
func filmShotOfScene(gap string) bool {
	g := strings.TrimSpace(gap)
	return utf8.RuneCountInString(g) <= 6 && (strings.HasPrefix(g, "ของ") || strings.HasPrefix(g, "ใน") || strings.EqualFold(g, "of") || strings.EqualFold(g, "in"))
}

func filmMentionNumber(n int) string {
	if n < 0 {
		return "ที่ระบุ"
	}
	return strconv.Itoa(n)
}

// ---------- Assistant operations ----------

type filmPlanOp struct {
	Op        string        `json:"op"` // add | delete | edit | voice
	Scene     int           `json:"scene,omitempty"`
	Shot      int           `json:"shot,omitempty"`
	NewScene  bool          `json:"newScene,omitempty"`
	Data      *filmPlanShot `json:"data,omitempty"`
	Character string        `json:"character,omitempty"`
	Voice     string        `json:"voice,omitempty"`
}

// filmEditOp is an operation resolved to stored ids, so it still applies after confirmation.
type filmEditOp struct {
	Op     string `json:"op"`
	Label  string `json:"label"`
	ShotID string `json:"shotId,omitempty"` // edit, delete
	// Before is the stored shot an added shot goes in front of ("" = the end). Scene is the scene
	// the new shot belongs to: "s<N>" (stored scene N) or "n<N>" (a new scene in front of scene N).
	Before string               `json:"before,omitempty"`
	Scene  string               `json:"scene,omitempty"`
	Shot   *req.FilmShotRequest `json:"shot,omitempty"`
	Voice  *filmEditVoiceTarget `json:"voice,omitempty"`
}

type filmEditVoiceTarget struct {
	CharacterID string          `json:"characterId"`
	Name        string          `json:"name"`
	VoiceName   string          `json:"voiceName"`
	Voice       json.RawMessage `json:"voice"`
}

// filmPendingEdit is an assistant edit waiting for the user's confirmation.
type filmPendingEdit struct {
	ID      string       `json:"id"`
	Ops     []filmEditOp `json:"ops"`
	Story   *filmStory   `json:"story,omitempty"`
	Reply   string       `json:"reply"`
	Deletes []string     `json:"deletes,omitempty"`
	// Outdated lists positions whose paid clip the edit makes out of date without being asked to.
	Outdated    []string               `json:"outdated,omitempty"`
	Voices      []dto.FilmPendingVoice `json:"voices,omitempty"`
	RedubPoints int                    `json:"redubPoints,omitempty"`
	VideoPoints int                    `json:"videoPoints,omitempty"`
	// Stale counts continuation clips that a new video makes out of date (they follow its last frame).
	Stale int `json:"stale,omitempty"`
	// Remake lists the shots that get a new video for the voice change, decided when the card was
	// made (later the stored voices are already the new ones and older clips no longer match).
	Remake []string `json:"remake,omitempty"`
	// Layout is the shot order the edit was made on; confirming on another layout is refused.
	Layout    string    `json:"layout"`
	CreatedAt time.Time `json:"createdAt"`
}

// filmEditError is a position the assistant named that is not in the episode.
type filmEditError struct{ Message string }

func (e *filmEditError) Error() string { return e.Message }

// filmLayout identifies the shot order and scene split positions refer to.
func filmLayout(shots []filmShot) string {
	parts := make([]string, len(shots))
	for i := range shots {
		parts[i] = shots[i].ID + ":" + shots[i].Transition
	}
	return storyHash(parts)
}

// filmMergeShot lays an edit over the current shot: fields the assistant left out stay as they are.
func filmMergeShot(cur, edit filmPlanShot) filmPlanShot {
	out := cur
	if edit.Seconds > 0 {
		out.Seconds = edit.Seconds
	}
	if edit.Characters != nil {
		out.Characters = edit.Characters
	}
	if strings.TrimSpace(edit.Location) != "" {
		out.Location = edit.Location
	}
	if edit.Props != nil {
		out.Props = edit.Props
	}
	if strings.TrimSpace(edit.Action) != "" {
		out.Action = edit.Action
	}
	if strings.TrimSpace(edit.Camera) != "" {
		out.Camera = edit.Camera
	}
	if edit.Dialogue != nil {
		out.Dialogue = edit.Dialogue
	}
	if strings.TrimSpace(edit.Audio) != "" {
		out.Audio = edit.Audio
	}
	if strings.TrimSpace(edit.Blocking) != "" {
		out.Blocking = edit.Blocking
	}
	if edit.Cuts != nil {
		out.Cuts = edit.Cuts
	}
	return out
}

func filmClampSeconds(r *req.FilmShotRequest) {
	if r.Seconds == 0 {
		r.Seconds = 5
	}
	r.Seconds = min(max(r.Seconds, filmMinSeconds), filmMaxSeconds)
}

// filmResolveOps checks the assistant's operations against the episode (numbering before any
// operation) and ties them to stored shots.
func filmResolveOps(ops []filmPlanOp, d *filmDocument, resolve func(kind, name string) string, byID map[string]*models.FilmElement) ([]filmEditOp, error) {
	if len(ops) > filmMaxOps {
		return nil, &filmEditError{"คำสั่งนี้เปลี่ยนหลายตำแหน่งเกินไป ช่วยแบ่งเป็นทีละฉาก"}
	}
	scenes := filmScenes(d.Shots)
	shotAt := func(scene, shot int) (*filmShot, bool) {
		if scene < 1 || scene > len(scenes) || shot < 1 || shot > len(scenes[scene-1]) {
			return nil, false
		}
		return &d.Shots[scenes[scene-1][shot-1]], true
	}
	missing := func(scene, shot int) error {
		if scene < 1 || scene > len(scenes) {
			return &filmEditError{fmt.Sprintf("ผู้ช่วยอ้างถึงฉาก %d ซึ่งไม่มีในตอนนี้ (มี %d ฉาก) ช่วยระบุตำแหน่งอีกครั้ง", scene, len(scenes))}
		}
		return &filmEditError{fmt.Sprintf("ผู้ช่วยอ้างถึง %s ซึ่งไม่มีในตอนนี้ (ฉาก %d มี %d ช็อต) ช่วยระบุตำแหน่งอีกครั้ง", filmPosLabel(scene, shot), scene, len(scenes[scene-1]))}
	}
	newShot := func(p filmPlanShot) *req.FilmShotRequest {
		r := filmResolveShot(p, "", false, resolve)
		filmClampSeconds(&r)
		return &r
	}
	out := []filmEditOp{}
	touched := map[string]string{}
	for _, op := range ops {
		switch strings.ToLower(strings.TrimSpace(op.Op)) {
		case "edit":
			sh, ok := shotAt(op.Scene, op.Shot)
			if !ok {
				return nil, missing(op.Scene, op.Shot)
			}
			if op.Data == nil {
				return nil, &filmEditError{"ผู้ช่วยไม่ได้ส่งรายละเอียดของ " + filmPosLabel(op.Scene, op.Shot) + " กรุณาลองใหม่"}
			}
			if touched[sh.ID] != "" {
				continue
			}
			touched[sh.ID] = "edit"
			r := newShot(filmMergeShot(filmPlanShotOf(sh, byID), *op.Data))
			r.ID = sh.ID
			out = append(out, filmEditOp{Op: "edit", Label: filmPosLabel(op.Scene, op.Shot), ShotID: sh.ID, Shot: r})
		case "delete":
			if op.Shot == 0 {
				if op.Scene < 1 || op.Scene > len(scenes) {
					return nil, missing(op.Scene, 0)
				}
				for _, k := range scenes[op.Scene-1] {
					id := d.Shots[k].ID
					if touched[id] != "delete" {
						touched[id] = "delete"
						out = append(out, filmEditOp{Op: "delete", Label: filmPosLabel(op.Scene, filmPosOf(scenes, k).Shot), ShotID: id})
					}
				}
				continue
			}
			sh, ok := shotAt(op.Scene, op.Shot)
			if !ok {
				return nil, missing(op.Scene, op.Shot)
			}
			if touched[sh.ID] == "delete" {
				continue
			}
			touched[sh.ID] = "delete"
			out = append(out, filmEditOp{Op: "delete", Label: filmPosLabel(op.Scene, op.Shot), ShotID: sh.ID})
		case "add":
			if op.Data == nil {
				return nil, &filmEditError{"ผู้ช่วยไม่ได้ส่งรายละเอียดช็อตที่จะเพิ่ม กรุณาลองใหม่"}
			}
			e := filmEditOp{Op: "add", Shot: newShot(*op.Data)}
			if op.NewScene || op.Scene == len(scenes)+1 {
				if op.Scene < 1 || op.Scene > len(scenes)+1 {
					return nil, missing(op.Scene, 0)
				}
				e.Scene = fmt.Sprintf("n%d", op.Scene)
				e.Label = fmt.Sprintf("ฉากใหม่ก่อนฉาก %d", op.Scene)
				if op.Scene <= len(scenes) {
					e.Before = d.Shots[scenes[op.Scene-1][0]].ID
				} else {
					e.Label = fmt.Sprintf("ฉาก %d (ฉากใหม่ต่อท้าย)", op.Scene)
				}
			} else {
				if op.Scene < 1 || op.Scene > len(scenes) {
					return nil, missing(op.Scene, 0)
				}
				size := len(scenes[op.Scene-1])
				if op.Shot < 1 || op.Shot > size+1 {
					return nil, missing(op.Scene, op.Shot)
				}
				if op.Shot <= size {
					e.Before = d.Shots[scenes[op.Scene-1][op.Shot-1]].ID
				} else if op.Scene < len(scenes) {
					e.Before = d.Shots[scenes[op.Scene][0]].ID
				}
				e.Scene = fmt.Sprintf("s%d", op.Scene)
				e.Label = filmPosLabel(op.Scene, op.Shot) + " (ใหม่)"
			}
			out = append(out, e)
		case "voice":
			id := resolve("character", op.Character)
			if id == "" {
				return nil, &filmEditError{fmt.Sprintf("ไม่พบตัวละคร “%s” ช่วยระบุชื่อตัวละครให้ตรงกับในแท็บองค์ประกอบ", op.Character)}
			}
			voice := filmPlanVoice(op.Voice)
			if voice == nil {
				return nil, &filmEditError{"ผู้ช่วยเลือกเสียงที่ไม่มีในรายการ ช่วยบอกลักษณะเสียงที่ต้องการอีกครั้ง"}
			}
			v := models.DecodeCharacterVoice(voice)
			name := op.Character
			if el := byID[id]; el != nil {
				name = el.Name
				// Same preset as now: nothing to change (and nothing to pay for).
				if old := filmBoundVoice(el); old != nil && old.Kind == v.Kind && old.VoiceID == v.VoiceID {
					continue
				}
			}
			if touched["voice:"+id] != "" {
				continue
			}
			touched["voice:"+id] = "voice"
			out = append(out, filmEditOp{Op: "voice", Label: "เสียงของ @" + name, Voice: &filmEditVoiceTarget{CharacterID: id, Name: name, VoiceName: v.VoiceName, Voice: voice}})
		}
	}
	// An edit of a shot that is also deleted is dropped.
	kept := out[:0]
	for _, op := range out {
		if op.Op == "edit" && touched[op.ShotID] == "delete" {
			continue
		}
		kept = append(kept, op)
	}
	return kept, nil
}

// filmApplyOps applies the structural operations (add, delete, edit) to the current shots. A shot
// continues from the previous one exactly when both belong to the same scene (stored scenes keep
// their shots; added shots name their scene) and stand in the same place.
func filmApplyOps(cur []filmShot, ops []filmEditOp) ([]req.FilmShotRequest, error) {
	scenes := filmScenes(cur)
	sceneOf := map[string]string{}
	for n, sc := range scenes {
		for _, k := range sc {
			sceneOf[cur[k].ID] = fmt.Sprintf("s%d", n+1)
		}
	}
	edits, deletes := map[string]*req.FilmShotRequest{}, map[string]bool{}
	adds := map[string][]filmEditOp{}
	for _, op := range ops {
		switch op.Op {
		case "edit":
			if sceneOf[op.ShotID] == "" {
				return nil, &filmEditError{op.Label + " ถูกแก้หรือลบไปแล้ว กรุณาสั่งใหม่"}
			}
			edits[op.ShotID] = op.Shot
		case "delete":
			if sceneOf[op.ShotID] == "" {
				return nil, &filmEditError{op.Label + " ถูกลบไปแล้ว กรุณาสั่งใหม่"}
			}
			deletes[op.ShotID] = true
		case "add":
			if op.Before != "" && sceneOf[op.Before] == "" {
				return nil, &filmEditError{"ตำแหน่ง " + op.Label + " เปลี่ยนไปแล้ว กรุณาสั่งใหม่"}
			}
			adds[op.Before] = append(adds[op.Before], op)
		}
	}
	// In front of one shot: shots that end the previous scene, then new scenes, then shots that
	// join the shot's own scene.
	for before, list := range adds {
		own := sceneOf[before]
		rank := func(op filmEditOp) int {
			switch {
			case strings.HasPrefix(op.Scene, "n"):
				return 1
			case op.Scene == own:
				return 2
			}
			return 0
		}
		sort.SliceStable(list, func(a, b int) bool { return rank(list[a]) < rank(list[b]) })
	}
	type item struct {
		r     req.FilmShotRequest
		scene string
		fresh bool   // added or rewritten
		was   string // stored transition ("" for added shots)
	}
	out := []item{}
	emit := func(list []filmEditOp) {
		for _, op := range list {
			r := *op.Shot
			r.ID = ""
			out = append(out, item{r: r, scene: op.Scene, fresh: true})
		}
	}
	for i := range cur {
		sh := &cur[i]
		emit(adds[sh.ID])
		if deletes[sh.ID] {
			continue
		}
		r := filmShotRequest(sh)
		fresh := false
		if e := edits[sh.ID]; e != nil {
			r, fresh = *e, true
			r.ID = sh.ID
		}
		out = append(out, item{r: r, scene: sceneOf[sh.ID], fresh: fresh, was: sh.Transition})
	}
	emit(adds[""])
	if len(out) == 0 {
		return nil, &filmEditError{"คำสั่งนี้จะลบทุกช็อตออก ถ้าต้องการเริ่มใหม่ ให้ลบช็อตในสตูดิโอแล้ววางเรื่องใหม่"}
	}
	shots := make([]req.FilmShotRequest, len(out))
	for i := range out {
		r := out[i].r
		r.Transition = "cut"
		if i > 0 && out[i-1].scene == out[i].scene {
			r.Transition = "continue"
			// A shot cannot continue from a frame in another place (untouched shots keep theirs).
			if (out[i].fresh || out[i].was != "continue" || out[i-1].fresh) && r.LocationID != out[i-1].r.LocationID {
				r.Transition = "cut"
			}
		}
		shots[i] = r
	}
	return shots, nil
}

// filmPaidClip tells whether a shot has a clip or draft the user paid for.
func filmPaidClip(d *filmDocument, sh *filmShot) bool {
	if sh.Video != nil && sh.Video.Status == "succeeded" {
		return true
	}
	_, ok := d.Drafts[sh.ID]
	return ok
}

// filmEditLosses lists, by their position before the edit, the paid clips an edit deletes and
// the ones it makes out of date without being asked to (a new transition or a new previous shot).
func filmEditLosses(d *filmDocument, next []filmShot, ops []filmEditOp, byID map[string]*models.FilmElement, style string) (deletes, outdated []string) {
	scenes := filmScenes(d.Shots)
	edited := map[string]bool{}
	for _, op := range ops {
		if op.Op == "delete" {
			if _, sh := filmFindShot(d, op.ShotID); sh != nil && filmPaidClip(d, sh) {
				deletes = append(deletes, op.Label)
			}
		}
		if op.Op == "edit" {
			edited[op.ShotID] = true
		}
	}
	for i := range next {
		k, old := filmFindShot(d, next[i].ID)
		if old == nil || edited[old.ID] || !filmPaidClip(d, old) {
			continue
		}
		changed := filmShotHash(old, byID, style) != filmShotHash(&next[i], byID, style)
		if next[i].Transition == "continue" {
			prevOld, prevNew := "", ""
			if k > 0 {
				prevOld = d.Shots[k-1].ID
			}
			if i > 0 {
				prevNew = next[i-1].ID
			}
			changed = changed || prevOld != prevNew
		}
		if changed {
			p := filmPosOf(scenes, k)
			outdated = append(outdated, filmPosLabel(p.Scene, p.Shot))
		}
	}
	return deletes, outdated
}

// filmVoiceImpact prices a voice change from the voices now (before) to the new ones (after).
// Clips that keep their raw video are re-dubbed (every out-of-date clip of the episode: that is
// what /redub does); clips whose voice is part of the video get a new video (decided: re-made
// automatically after confirmation). It returns the re-dub clips and points, the video points,
// the shot ids to re-make and how many later continuation clips a new video makes stale.
func (f *FilmFacade) filmVoiceImpact(d *filmDocument, before, after map[string]*models.FilmElement, style string, characterIDs map[string]bool) (redubClips, redubPoints, videoPoints int, remake []string, stale int) {
	scenes := filmScenes(d.Shots)
	staleSeen, remade := map[string]bool{}, map[string]bool{}
	for i := range d.Shots {
		sh := &d.Shots[i]
		v := sh.Video
		// Freshness is judged on today's voices: older clips carried them in their hash.
		if v == nil || v.Status != "succeeded" || !filmVideoFresh(v, sh, before, style) {
			continue
		}
		lines := filmVoiceLines(sh, after)
		st := filmVoiceStatus(v, sh, after, style)
		if st == "outdated" || st == "failed" {
			redubClips++
			redubPoints += filmVoicePoints(lines)
			continue
		}
		if st != "legacy" && st != "overlap" {
			continue
		}
		speaks := false
		for _, l := range sh.Dialogue {
			speaks = speaks || characterIDs[l.SpeakerID]
		}
		if !speaks {
			continue
		}
		rate := f.options.VideoPoints
		if v.Draft && f.options.DraftPoints > 0 {
			rate = f.options.DraftPoints
		}
		videoPoints += filmVideoCost(rate, sh.Seconds) + filmVoicePoints(lines)
		remake, remade[sh.ID] = append(remake, sh.ID), true
		// Later shots of the scene start on this clip's last frame.
		pos := filmPosOf(scenes, i)
		for _, k := range scenes[pos.Scene-1][pos.Shot:] {
			if o := d.Shots[k].Video; o != nil && o.Status == "succeeded" {
				staleSeen[d.Shots[k].ID] = true
			}
		}
	}
	for id := range remade {
		delete(staleSeen, id)
	}
	if !f.redubAvailable() {
		redubClips, redubPoints = 0, 0
	}
	return redubClips, redubPoints, videoPoints, remake, len(staleSeen)
}

// filmVoicesAfter is the element map with the voice operations applied.
func filmVoicesAfter(byID map[string]*models.FilmElement, ops []filmEditOp) (map[string]*models.FilmElement, map[string]bool) {
	after, changed := map[string]*models.FilmElement{}, map[string]bool{}
	for k, v := range byID {
		after[k] = v
	}
	for _, op := range ops {
		if op.Op != "voice" {
			continue
		}
		if el := after[op.Voice.CharacterID]; el != nil {
			cp := *el
			cp.Voice = op.Voice.Voice
			after[op.Voice.CharacterID] = &cp
		}
		changed[op.Voice.CharacterID] = true
	}
	return after, changed
}

// filmOpsPrompt is the edit prompt of an episode that already has shots.
func filmOpsPrompt(in *filmPlanJob) string {
	ctx, _ := json.Marshal(struct {
		Story    *filmStory        `json:"story,omitempty"`
		Elements []filmPlanElement `json:"elements"`
		Shots    []filmPlanShot    `json:"shots"`
		History  []filmMessage     `json:"conversation"`
	}{in.Story, in.Elements, in.Shots, in.History})
	style := directorStyleTerms[in.Style]
	if style == "" {
		style = in.Style
	}
	var b strings.Builder
	b.WriteString("You are a film director and screenwriter editing an existing episode. The episode is generated shot by shot by a reference-to-video AI model that receives each element's reference image, so every shot must be self-contained and consistent.\n")
	fmt.Fprintf(&b, "Visual style: %s. Aspect ratio %s.\n", style, in.Ratio)
	b.WriteString("Positions: every shot has \"scene\" (1-based) and \"shot\" (1-based, numbered again inside each scene). The user and you always name a position as \"ฉาก N · ช็อต M\" (or \"ฉาก N\" for a whole scene). Every position in your answer refers to the numbering BEFORE any of your operations.\n")
	if in.Target != "" {
		fmt.Fprintf(&b, "The user selected %s in the timeline: change only that unless the request names other positions.\n", in.Target)
	}
	b.WriteString(`Task: answer with the smallest list of operations that does what the user asks. Never return the whole shot list. Operations:
- {"op":"edit","scene":N,"shot":M,"data":{...}} changes that shot. data holds ONLY the fields you change (for example just "camera" and "action", or just "dialogue"); fields you leave out stay as they are. Keep its seconds unless asked.
- {"op":"delete","scene":N,"shot":M} removes a shot; {"op":"delete","scene":N} removes the whole scene.
- {"op":"add","scene":N,"shot":M,"data":SHOT} inserts a new shot that becomes ฉาก N · ช็อต M of that scene (it goes in front of the current shot M; M = the scene's shot count + 1 adds it at the end of the scene). Several adds with the same scene and shot are inserted in order.
- {"op":"add","scene":N,"newScene":true,"data":SHOT} inserts a new scene in front of the current scene N (N = scene count + 1 adds it at the end); more adds with the same scene and newScene continue that new scene in order.
- {"op":"voice","character":"name","voice":"preset id"} gives a character another preset voice (the server re-dubs its clips).
If the request is ambiguous (you cannot tell which position, what to change, or it would rewrite the whole film without naming scenes), do not guess: return "question" (Thai, one short question) and an empty "ops".
`)
	b.WriteString(filmPlanRulesText(in.Filmed) + "\n" + filmSkillsText(in.Skills))
	shotJSON := `{"seconds":5,"characters":["names"],"location":"name","props":["names"],"blocking":"English","action":"English","camera":"English","cuts":[{"camera":"English","action":"English"}],"dialogue":[{"speaker":"name","line":"Thai","emotion":"one listed emotion","delivery":"English, short"}],"audio":"English"}`
	b.WriteString(`Return exactly JSON: {"reply":"Thai, 1-3 sentences naming every position you changed as ฉาก N · ช็อต M","question":"","title":"Thai, only if the story changed","logline":"","synopsis":"","theme":"","elements":[{"kind":"character|location|prop","name":"Thai","spec":"English, <=600 chars","voice":"preset id, characters only"}],"ops":[{"op":"edit","scene":1,"shot":1,"data":{"camera":"English"}},{"op":"add","scene":1,"shot":2,"data":` + shotJSON + `}]}` + "\n")
	fmt.Fprintf(&b, "Current episode data: %s\n", ctx)
	fmt.Fprintf(&b, "User request: %s\n", in.Prompt)
	return b.String()
}

// planAsk ends a plan job with a question instead of an edit; the plan's points are refunded.
func (f *FilmFacade) planAsk(c context.Context, j *models.FilmJob, question, target string) error {
	return f.store.CompleteEpisode(c, j, "failed", true, func(ep *models.FilmEpisode) error {
		d, e := filmDecode(ep)
		if e != nil {
			return e
		}
		if d.Plan == nil || d.Plan.JobID != j.ID.String() {
			return nil
		}
		d.Plan = nil
		filmAddMessage(&d, filmMessage{Role: "assistant", Text: question + " (ไม่คิดแต้ม)", Target: target, At: time.Now().UTC()})
		return filmEncode(ep, &d)
	})
}

// planOpsJob finishes an operations plan job (see planJob).
func (f *FilmFacade) planOpsJob(c context.Context, j *models.FilmJob, in *filmPlanJob, out *filmPlanOutput) error {
	if q := strings.TrimSpace(out.Question); q != "" && len(out.Ops) == 0 {
		return f.planAsk(c, j, q, in.Target)
	}
	unclear := "ผู้ช่วยยังไม่เข้าใจว่าต้องแก้อะไร ช่วยบอกตำแหน่ง (ฉาก N · ช็อต M) และสิ่งที่ต้องการเปลี่ยนอีกครั้ง"
	if len(out.Ops) == 0 {
		return f.planAsk(c, j, unclear, in.Target)
	}
	var ask *filmEditError
	var direct []filmEditOp
	e := f.store.CompletePlan(c, j, func(ep *models.FilmEpisode, elements []models.FilmElement, s *models.DirectorSeries) ([]models.FilmElement, error) {
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		if d.Plan == nil || d.Plan.JobID != j.ID.String() {
			return nil, nil
		}
		created, resolve := filmPlanElements(j, out.Elements, elements)
		all := append(append([]models.FilmElement{}, elements...), created...)
		byID := filmElementMap(all)
		fail := func(e error) ([]models.FilmElement, error) {
			if fe, ok := e.(*filmEditError); ok {
				ask = fe
			}
			return nil, e
		}
		ops, e := filmResolveOps(out.Ops, &d, resolve, byID)
		if e != nil {
			return fail(e)
		}
		if len(ops) == 0 {
			return fail(&filmEditError{unclear})
		}
		structural, voices := []filmEditOp{}, []filmEditOp{}
		for _, op := range ops {
			if op.Op == "voice" {
				voices = append(voices, op)
			} else {
				structural = append(structural, op)
			}
		}
		valid := d.Shots
		if len(structural) > 0 {
			shots, e := filmApplyOps(d.Shots, structural)
			if e != nil {
				return fail(e)
			}
			if valid, e = filmValidateShots(shots, d.Shots, byID, s.Style); e != nil {
				return nil, e
			}
		}
		story := filmNextStory(d.Story, out)
		reply := strings.TrimSpace(out.Reply)
		if reply == "" {
			reply = "เรียบร้อยแล้ว"
		}
		// Confirmation: losing or outdating a paid clip, or a voice change that costs points.
		pending := &filmPendingEdit{ID: uuid.NewString(), Ops: ops, Story: story, Reply: reply, Layout: filmLayout(d.Shots), CreatedAt: time.Now().UTC()}
		pending.Deletes, pending.Outdated = filmEditLosses(&d, valid, structural, byID, s.Style)
		if len(voices) > 0 {
			after, changed := filmVoicesAfter(byID, voices)
			preview := filmDocument{Shots: valid, Drafts: d.Drafts}
			for _, op := range voices {
				one := map[string]bool{op.Voice.CharacterID: true}
				_, _, _, remake, _ := f.filmVoiceImpact(&preview, byID, after, s.Style, one)
				clips := 0
				for i := range preview.Shots {
					sh := &preview.Shots[i]
					speaks := false
					for _, l := range sh.Dialogue {
						speaks = speaks || one[l.SpeakerID]
					}
					if speaks && sh.Video != nil && sh.Video.Status == "succeeded" && filmVideoFresh(sh.Video, sh, byID, s.Style) && filmVoiceStatus(sh.Video, sh, after, s.Style) == "outdated" {
						clips++
					}
				}
				if !f.redubAvailable() {
					clips = 0
				}
				pending.Voices = append(pending.Voices, dto.FilmPendingVoice{Name: op.Voice.Name, Voice: op.Voice.VoiceName, RedubClips: clips, RemakeClips: len(remake)})
			}
			_, pending.RedubPoints, pending.VideoPoints, pending.Remake, pending.Stale = f.filmVoiceImpact(&preview, byID, after, s.Style, changed)
		}
		summary := filmOpsSummary(ops)
		if len(pending.Deletes) > 0 || len(pending.Outdated) > 0 || pending.RedubPoints+pending.VideoPoints > 0 {
			d.Pending = pending
			filmAddMessage(&d, filmMessage{Role: "assistant", Text: reply + " · " + summary + " · รอยืนยันก่อนแก้", Target: in.Target, At: time.Now().UTC()})
		} else {
			d.Shots, d.Story = valid, story
			// Voices that change no paid clip are set right after this transaction.
			direct = voices
			filmAddMessage(&d, filmMessage{Role: "assistant", Text: reply + " · " + summary + filmCreatedNote(len(created)), Target: in.Target, At: time.Now().UTC()})
		}
		d.Plan = nil
		return created, filmEncode(ep, &d)
	})
	if ask != nil {
		return f.planAsk(c, j, ask.Message, in.Target)
	}
	if e != nil {
		return f.planFail(c, j, "จัดช็อตจากคำตอบของผู้ช่วยไม่สำเร็จ กรุณาลองใหม่", e)
	}
	if len(direct) > 0 && j.ProjectID != nil {
		// The job is finished; a failure here leaves the old voice, which the user can set by hand.
		if e = f.applyVoiceOps(c, j.ProjectID.String(), j.UserID.String(), j.ID.String(), direct, nil); e != nil {
			log.Printf("film: plan job=%s voice change failed: %v", j.ID, e)
		}
	}
	return nil
}

func filmCreatedNote(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" · เพิ่มองค์ประกอบใหม่ %d รายการ กดสร้างภาพในแท็บองค์ประกอบก่อนสร้างวิดีโอ", n)
}

// filmOpsSummary lists the operations by position, e.g. "แก้ ฉาก 2 · ช็อต 1 · ลบ ฉาก 3 · ช็อต 2".
func filmOpsSummary(ops []filmEditOp) string {
	verbs := map[string]string{"edit": "แก้", "delete": "ลบ", "add": "เพิ่ม", "voice": "เปลี่ยน"}
	parts := []string{}
	for _, op := range ops {
		parts = append(parts, verbs[op.Op]+" "+op.Label)
	}
	return strings.Join(parts, " · ")
}

// filmNextStory merges the story fields of an answer into the current story (nil: unchanged).
func filmNextStory(cur *filmStory, out *filmPlanOutput) *filmStory {
	if strings.TrimSpace(out.Title) == "" && strings.TrimSpace(out.Synopsis) == "" && strings.TrimSpace(out.Logline) == "" && strings.TrimSpace(out.Theme) == "" {
		return cur
	}
	next := filmStory{Title: directorClipRunes(out.Title, 120), Logline: directorClipRunes(out.Logline, 600), Synopsis: directorClipRunes(out.Synopsis, 1200), Theme: directorClipRunes(out.Theme, 400)}
	if cur != nil {
		if next.Title == "" {
			next.Title = cur.Title
		}
		if next.Logline == "" {
			next.Logline = cur.Logline
		}
		if next.Synopsis == "" {
			next.Synopsis = cur.Synopsis
		}
		if next.Theme == "" {
			next.Theme = cur.Theme
		}
	}
	return &next
}

// ---------- Confirmation ----------

func filmPlanStale() error {
	return &DirectorError{Status: 409, Code: "PLAN_STALE", Message: "รายการแก้ไขนี้หมดอายุแล้ว (ตอนถูกแก้หลังจากนั้น) กรุณาสั่งใหม่ในแชต"}
}

// ConfirmPlan applies (accept) or drops a pending assistant edit. Accepting a voice change first
// sets the voice, re-dubs the clips that keep their raw video and queues a new video for clips
// whose voice is part of the video; only then is the edit marked done, so a failure (points, a
// busy shot) leaves the card in place and a retry finishes the work (every job has a key derived
// from the confirmation key, and queued work is not a finished clip any more, so it is not redone).
func (f *FilmFacade) ConfirmPlan(c context.Context, projectID, user string, in req.FilmPlanConfirmRequest) (dto.FilmEpisode, error) {
	if !filmUUID(projectID) {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	if !filmUUID(in.IdempotencyKey) || !filmUUID(in.PendingID) {
		return dto.FilmEpisode{}, directorInvalid("idempotencyKey")
	}
	if in.Accept {
		_, _, ep, e := f.store.Episode(c, projectID, user)
		if e != nil {
			return dto.FilmEpisode{}, e
		}
		d, e := filmDecode(ep)
		if e != nil {
			return dto.FilmEpisode{}, e
		}
		// Without the pending edit this is a replay (or stale): ChangeEpisode below answers it.
		if p := d.Pending; p != nil && p.ID == in.PendingID {
			if p.Layout != filmLayout(d.Shots) {
				return dto.FilmEpisode{}, filmPlanStale()
			}
			voices, structural := []filmEditOp{}, []filmEditOp{}
			for _, op := range p.Ops {
				if op.Op == "voice" {
					voices = append(voices, op)
				} else {
					structural = append(structural, op)
				}
			}
			if len(voices) > 0 && len(structural) > 0 {
				// Check the shot edit before anything is charged: it must still apply as shown.
				_, s, _, e := f.store.Episode(c, projectID, user)
				if e != nil {
					return dto.FilmEpisode{}, e
				}
				elements, e := f.store.Elements(c, s.ID.String(), user)
				if e != nil {
					return dto.FilmEpisode{}, e
				}
				byID := filmElementMap(elements)
				shots, e := filmApplyOps(d.Shots, structural)
				if e != nil {
					return dto.FilmEpisode{}, filmPlanStale()
				}
				valid, e := filmValidateShots(shots, d.Shots, byID, s.Style)
				if e != nil {
					return dto.FilmEpisode{}, e
				}
				if deletes, _ := filmEditLosses(&d, valid, structural, byID, s.Style); len(deletes) > len(p.Deletes) {
					return dto.FilmEpisode{}, filmPlanStale()
				}
			}
			if len(voices) > 0 {
				need := int64(p.RedubPoints + p.VideoPoints)
				if balance, e := f.store.Balance(c, user); e == nil && balance < need {
					return dto.FilmEpisode{}, &DirectorError{Status: 402, Code: "INSUFFICIENT_POINTS", Message: fmt.Sprintf("แต้มไม่พอ ต้องใช้ %d แต้ม คงเหลือ %d แต้ม", need, balance)}
				}
				if e = f.applyVoiceOps(c, projectID, user, in.IdempotencyKey, voices, p.Remake); e != nil {
					return dto.FilmEpisode{}, e
				}
			}
		}
	}
	hash := storyHash(struct {
		Action, Project string
		Input           req.FilmPlanConfirmRequest
	}{"film-plan-confirm", projectID, in})
	_, e := f.store.ChangeEpisode(c, projectID, user, in.IdempotencyKey, hash, func(ep *models.FilmEpisode, _ *models.DirectorProject, s *models.DirectorSeries, elements []models.FilmElement) ([]models.FilmJob, error) {
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		p := d.Pending
		if p == nil || p.ID != in.PendingID {
			return nil, &DirectorError{Status: 409, Code: "PLAN_STALE", Message: "รายการแก้ไขนี้หมดอายุแล้ว กรุณาสั่งใหม่ในแชต"}
		}
		if !in.Accept {
			d.Pending = nil
			filmAddMessage(&d, filmMessage{Role: "assistant", Text: "ยกเลิกการแก้ไขแล้ว ไม่มีอะไรเปลี่ยน", At: time.Now().UTC()})
			return nil, filmEncode(ep, &d)
		}
		if p.Layout != filmLayout(d.Shots) {
			return nil, filmPlanStale()
		}
		structural := []filmEditOp{}
		for _, op := range p.Ops {
			if op.Op != "voice" {
				structural = append(structural, op)
			}
		}
		if len(structural) > 0 {
			byID := filmElementMap(elements)
			shots, e := filmApplyOps(d.Shots, structural)
			if e != nil {
				if fe, ok := e.(*filmEditError); ok {
					return nil, &DirectorError{Status: 409, Code: "PLAN_STALE", Message: fe.Message}
				}
				return nil, e
			}
			valid, e := filmValidateShots(shots, d.Shots, byID, s.Style)
			if e != nil {
				return nil, e
			}
			// A clip finished after the card was shown would be lost without warning: ask again.
			if deletes, _ := filmEditLosses(&d, valid, structural, byID, s.Style); len(deletes) > len(p.Deletes) {
				return nil, filmPlanStale()
			}
			d.Shots = valid
		}
		d.Pending = nil
		d.Story = p.Story
		filmAddMessage(&d, filmMessage{Role: "assistant", Text: "ยืนยันแล้ว · " + filmOpsSummary(p.Ops), At: time.Now().UTC()})
		return nil, filmEncode(ep, &d)
	})
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	return f.GetEpisode(c, projectID, user)
}

func filmDerivedKey(key, scope string) string {
	return uuid.NewSHA1(filmEditNamespace, []byte(key+":"+scope)).String()
}

// applyVoiceOps sets the voices, re-dubs the clips that keep their raw video and queues a new
// video for the clips whose voice is part of the video: the shots in remake (a confirmed card,
// priced on the edited shots), or, when nil, the ones found now, before the voices change (older
// clips carry the voices in their hash). Every job has a key derived from key, so a retry replays
// work already queued instead of paying twice.
func (f *FilmFacade) applyVoiceOps(c context.Context, projectID, user, key string, ops []filmEditOp, remake []string) error {
	_, s, ep, e := f.store.Episode(c, projectID, user)
	if e != nil {
		return e
	}
	elements, e := f.store.Elements(c, s.ID.String(), user)
	if e != nil {
		return e
	}
	d, e := filmDecode(ep)
	if e != nil {
		return e
	}
	byID := filmElementMap(elements)
	after, changed := filmVoicesAfter(byID, ops)
	if remake == nil {
		_, _, _, remake, _ = f.filmVoiceImpact(&d, byID, after, s.Style, changed)
	}
	for _, op := range ops {
		v := op.Voice
		if _, e := f.store.ChangeElement(c, v.CharacterID, user, "", "", func(el *models.FilmElement, _ *models.DirectorSeries, _ repo.FilmAssetLoader) ([]models.FilmJob, error) {
			if el.Kind != "character" {
				return nil, directorInvalid("voice")
			}
			if string(el.Voice) != string(v.Voice) {
				el.Voice = v.Voice
				el.Revision++
			}
			return nil, nil
		}); e != nil {
			return e
		}
	}
	if f.redubAvailable() {
		if _, e = f.RequestRedub(c, projectID, user, req.FilmRedubRequest{IdempotencyKey: filmDerivedKey(key, "redub")}); e != nil {
			if de, ok := e.(*DirectorError); !ok || de.Code != "NOTHING_TO_REDUB" {
				return e
			}
		}
	}
	for _, id := range remake {
		_, sh := filmFindShot(&d, id)
		if sh == nil || sh.Video == nil {
			continue
		}
		in := req.FilmVideoRequest{IdempotencyKey: filmDerivedKey(key, "video:"+id), ModelID: sh.Video.ModelID, Draft: sh.Video.Draft && f.options.DraftPoints > 0}
		if _, e = f.RequestVideo(c, projectID, id, user, in); e != nil {
			return e
		}
	}
	return nil
}

// filmPendingDTO presents a pending edit.
func filmPendingDTO(p *filmPendingEdit) *dto.FilmPending {
	if p == nil {
		return nil
	}
	return &dto.FilmPending{ID: p.ID, Reply: p.Reply, Summary: filmOpsSummary(p.Ops), Deletes: append([]string{}, p.Deletes...), Outdated: append([]string{}, p.Outdated...), Voices: append([]dto.FilmPendingVoice{}, p.Voices...), RedubPoints: p.RedubPoints, VideoPoints: p.VideoPoints, Stale: p.Stale, CreatedAt: p.CreatedAt}
}
