package facade_open_api

// Film chat planning (ภาพยนตร์ phase 3). A "plan" job asks the story LLM for the episode's story,
// elements and shots, or rewrites one shot; the result replaces the episode's shots and adds any
// new elements. Isolated from Director: it only calls Director's JSON decoder and text clipper.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/google/uuid"
)

const (
	filmPromptMax        = 2000
	filmMessagesMax      = 80
	filmTargetMin        = 10
	filmTargetMax        = 300
	filmTargetDefault    = 60
	filmPlanMaxElements  = 14
	filmPlanHistoryTurns = 8
)

type filmStory struct {
	Title    string `json:"title"`
	Logline  string `json:"logline"`
	Synopsis string `json:"synopsis"`
	Theme    string `json:"theme"`
}

type filmMessage struct {
	Role   string    `json:"role"` // user | assistant
	Text   string    `json:"text"`
	ShotID string    `json:"shotId,omitempty"`
	At     time.Time `json:"at"`
}

type filmPlanState struct {
	Status         string  `json:"status"` // queued | processing | failed
	JobID          string  `json:"jobId"`
	ShotID         string  `json:"shotId,omitempty"`
	FailureMessage *string `json:"failureMessage,omitempty"`
}

// LLM schema.
type filmPlanLine struct {
	Speaker  string `json:"speaker"`
	Line     string `json:"line"`
	Delivery string `json:"delivery"`
	Emotion  string `json:"emotion,omitempty"`
}
type filmPlanShot struct {
	// No is an existing shot's 1-based number (context only); Keep, in an answer, reuses that shot
	// unchanged so its video survives; Scene groups shots: a new scene number means a cut.
	No         int            `json:"no,omitempty"`
	Keep       int            `json:"keep,omitempty"`
	Scene      int            `json:"scene,omitempty"`
	Seconds    int            `json:"seconds"`
	Transition string         `json:"transition"`
	Characters []string       `json:"characters"`
	Location   string         `json:"location"`
	Props      []string       `json:"props"`
	Action     string         `json:"action"`
	Camera     string         `json:"camera"`
	Dialogue   []filmPlanLine `json:"dialogue"`
	Audio      string         `json:"audio"`
}
type filmPlanElement struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Spec string `json:"spec"`
	// Voice is a preset voice id the planner picks for a new character.
	Voice string `json:"voice,omitempty"`
}
type filmPlanOutput struct {
	Reply    string            `json:"reply"`
	Title    string            `json:"title"`
	Logline  string            `json:"logline"`
	Synopsis string            `json:"synopsis"`
	Theme    string            `json:"theme"`
	Elements []filmPlanElement `json:"elements"`
	Shots    []filmPlanShot    `json:"shots"`
	Shot     *filmPlanShot     `json:"shot"`
}

// filmPlanJob is the frozen input of a plan job.
type filmPlanJob struct {
	Prompt        string            `json:"prompt"`
	ShotID        string            `json:"shotId,omitempty"`
	ShotIndex     int               `json:"shotIndex"`
	TargetSeconds int               `json:"targetSeconds"`
	Style         string            `json:"style"`
	Filmed        bool              `json:"filmed"`
	Ratio         string            `json:"ratio"`
	Model         string            `json:"model"`
	Story         *filmStory        `json:"story,omitempty"`
	Elements      []filmPlanElement `json:"elements"`
	Shots         []filmPlanShot    `json:"shots"`
	// ShotIDs maps the context's shot numbers to stored shots, so "keep" survives later edits.
	ShotIDs []string      `json:"shotIds,omitempty"`
	History []filmMessage `json:"history"`
}

func filmAddMessage(d *filmDocument, m filmMessage) {
	d.Messages = append(d.Messages, m)
	if n := len(d.Messages); n > filmMessagesMax {
		d.Messages = append([]filmMessage(nil), d.Messages[n-filmMessagesMax:]...)
	}
}

func filmPlanActive(p *filmPlanState) bool {
	return p != nil && (p.Status == "queued" || p.Status == "processing")
}

// filmPlanShotOf describes a stored shot by element names, as the LLM sees it.
func filmPlanShotOf(sh *filmShot, byID map[string]*models.FilmElement) filmPlanShot {
	name := func(id string) string {
		if el, ok := byID[id]; ok {
			return el.Name
		}
		return ""
	}
	out := filmPlanShot{Seconds: sh.Seconds, Transition: sh.Transition, Location: name(sh.LocationID), Action: sh.Action, Camera: sh.Camera, Audio: sh.Audio, Characters: []string{}, Props: []string{}, Dialogue: []filmPlanLine{}}
	for _, id := range sh.CharacterIDs {
		out.Characters = append(out.Characters, name(id))
	}
	for _, id := range sh.PropIDs {
		out.Props = append(out.Props, name(id))
	}
	for _, l := range sh.Dialogue {
		out.Dialogue = append(out.Dialogue, filmPlanLine{Speaker: name(l.SpeakerID), Line: l.Line, Delivery: l.Delivery, Emotion: l.Emotion})
	}
	return out
}

func filmShotRequest(sh *filmShot) req.FilmShotRequest {
	lines := []req.FilmLineRequest{}
	for _, l := range sh.Dialogue {
		lines = append(lines, req.FilmLineRequest{SpeakerID: l.SpeakerID, Line: l.Line, Delivery: l.Delivery, Emotion: l.Emotion})
	}
	return req.FilmShotRequest{ID: sh.ID, Seconds: sh.Seconds, Transition: sh.Transition, CharacterIDs: append([]string{}, sh.CharacterIDs...), LocationID: sh.LocationID, PropIDs: append([]string{}, sh.PropIDs...), Action: sh.Action, Camera: sh.Camera, Dialogue: lines, Audio: sh.Audio, TrimIn: sh.TrimIn, TrimOut: sh.TrimOut, LineAt: append([]float64(nil), sh.LineAt...)}
}

func (f *FilmFacade) RequestPlan(c context.Context, projectID, user string, in req.FilmPlanRequest) (dto.FilmEpisode, error) {
	if !filmUUID(projectID) {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	if !filmUUID(in.IdempotencyKey) {
		return dto.FilmEpisode{}, directorInvalid("idempotencyKey")
	}
	prompt := strings.TrimSpace(in.Prompt)
	if !filmBound(prompt, 1, filmPromptMax) {
		return dto.FilmEpisode{}, directorInvalid("prompt")
	}
	if in.TargetSeconds != 0 && (in.TargetSeconds < filmTargetMin || in.TargetSeconds > filmTargetMax) {
		return dto.FilmEpisode{}, directorInvalid("targetSeconds")
	}
	if f.options.Analyzer == nil || f.options.AnalysisModel == "" {
		return dto.FilmEpisode{}, &DirectorError{Status: 503, Code: "PLAN_UNAVAILABLE", Message: "ระบบวางเรื่องยังไม่พร้อมใช้งาน"}
	}
	hash := storyHash(struct {
		Action, Project string
		Input           req.FilmPlanRequest
	}{"film-plan", projectID, in})
	_, e := f.store.ChangeEpisode(c, projectID, user, in.IdempotencyKey, hash, func(ep *models.FilmEpisode, _ *models.DirectorProject, s *models.DirectorSeries, elements []models.FilmElement) ([]models.FilmJob, error) {
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		if filmPlanActive(d.Plan) {
			return nil, &DirectorError{Status: 409, Code: "PLAN_BUSY", Message: "ผู้ช่วยกำลังวางเรื่องอยู่ กรุณารอสักครู่"}
		}
		byID := filmElementMap(elements)
		index := -1
		if in.ShotID != "" {
			i, sh := filmFindShot(&d, in.ShotID)
			if sh == nil {
				return nil, ErrFilmNotFound
			}
			if filmActive(sh.Video) {
				return nil, &DirectorError{Status: 409, Code: "SHOT_BUSY", Message: "ช็อตนี้กำลังสร้างวิดีโอ กรุณารอให้เสร็จก่อนแก้"}
			}
			index = i
		} else {
			for i := range d.Shots {
				if filmActive(d.Shots[i].Video) {
					return nil, &DirectorError{Status: 409, Code: "SHOT_BUSY", Message: "มีช็อตกำลังสร้างวิดีโอ กรุณารอให้เสร็จก่อนวางเรื่องใหม่"}
				}
			}
		}
		target := in.TargetSeconds
		if target == 0 {
			target = d.TargetSeconds
		}
		if target == 0 {
			target = filmTargetDefault
		}
		snap := filmPlanJob{Prompt: prompt, ShotID: in.ShotID, ShotIndex: index, TargetSeconds: target, Style: s.Style, Filmed: filmFilmed(s.Style), Ratio: s.AspectRatio, Model: f.options.AnalysisModel, Story: d.Story, Elements: []filmPlanElement{}, Shots: []filmPlanShot{}}
		for i := range elements {
			snap.Elements = append(snap.Elements, filmPlanElement{Kind: elements[i].Kind, Name: elements[i].Name, Spec: elements[i].Spec})
		}
		scene := 0
		for i := range d.Shots {
			if i == 0 || d.Shots[i].Transition != "continue" {
				scene++
			}
			p := filmPlanShotOf(&d.Shots[i], byID)
			p.No, p.Scene = i+1, scene
			snap.Shots = append(snap.Shots, p)
			snap.ShotIDs = append(snap.ShotIDs, d.Shots[i].ID)
		}
		history := d.Messages
		if len(history) > filmPlanHistoryTurns {
			history = history[len(history)-filmPlanHistoryTurns:]
		}
		snap.History = append([]filmMessage{}, history...)
		raw, e := json.Marshal(snap)
		if e != nil {
			return nil, e
		}
		cost := f.options.PlanPoints
		if in.ShotID != "" {
			cost = max(1, (f.options.PlanPoints+3)/4)
		}
		jobID := uuid.New()
		filmAddMessage(&d, filmMessage{Role: "user", Text: prompt, ShotID: in.ShotID, At: time.Now().UTC()})
		d.Plan = &filmPlanState{Status: "queued", JobID: jobID.String(), ShotID: in.ShotID}
		if in.ShotID == "" {
			d.TargetSeconds = target
		}
		if e = filmEncode(ep, &d); e != nil {
			return nil, e
		}
		project := ep.ProjectID
		return []models.FilmJob{{ID: jobID, SeriesID: s.ID, ProjectID: &project, Kind: "plan", TargetID: "plan:" + projectID, Snapshot: raw, Cost: cost}}, nil
	})
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	return f.GetEpisode(c, projectID, user)
}

const filmPlanRules = `Rules:
- Write title, logline, synopsis, theme and reply in Thai. Dialogue lines are natural spoken Thai. Everything else (spec, action, camera, audio, delivery) is English.
- Elements are every recurring character, location and important prop. Each has a short Thai name (used as @name for characters and #name for locations) and a detailed English visual spec: characters give age, gender, build, face, hair, outfit with exact colors and materials, and accessories; locations give architecture, layout, time of day, weather and light; props give the exact real-world structure. Never mention an art style or medium in a spec. Reuse existing elements by their exact name; add new ones only when needed. Do not create a prop for anything a character already wears or carries in its spec (such as a character's own sword); create a prop only for an object that is handed over, shown on its own, or shared by several characters.
- Shots: "action" is beat-by-beat motion in 40-120 English words naming characters by their element names; "camera" states shot size, angle and movement; list every visible character in "characters" (at most 4) and place them clearly; show at most two faces clearly and never stage faces touching; dialogue speakers must be listed in the shot's characters; "audio" lists sound effects, ambience and music mood.
- Think in scenes. A scene is one continuous moment in one location, usually told in 2-4 shots. Give every shot a "scene" number (1, 2, 3...). Shots after the first in a scene start exactly on the previous shot's last frame; a new angle, time or place starts a new scene. Each shot must add a new story beat; never write two near-identical shots in a row.
- Cast: every person or group who acts, fights, speaks or is acted upon on screen is listed in "characters"; never describe an off-screen attacker, opponent or crowd. Inside a scene, everyone visible at the end of a shot is still listed in the next shot unless the action shows them leave. A crowd that must be seen (villagers, an army) is its own character element.
- Dialogue emotion: every dialogue line gets "emotion", one of: %EMOTIONS%. Choose it from this shot's action, the scene's moment and the words themselves, never from the character alone: the same character whispers comfort in one scene and shouts a battle cry in the next. "delivery" adds the detail in English (pace, breath, volume, who they speak to).
- Voices: every new character element gets "voice", the id of the preset voice that best fits its gender, age and personality. Presets: %VOICES%.
- Props: an object a character holds stays in the same hands for the rest of the scene and is listed in every shot where it is visible; a boat sits in water, a ladder leans on a wall, and every object obeys real physics.
- Action: at most two clear beats per shot that a real stunt performer could do in its seconds; no superhuman leaps or acrobatics.
` + filmCameraRules + `
- Every shot is 4 to 15 seconds.
- You only write the plan. You cannot render images or video: if the user asks for a video or draft, plan or edit the shots and say in the reply that they can press the draft button for those shots.`

// filmPlanRulesText is filmPlanRules with the preset voice list filled in.
func filmPlanRulesText() string {
	labels := make([]string, 0, len(voiceEmotions))
	for _, e := range voiceEmotions {
		labels = append(labels, e.Label)
	}
	return strings.NewReplacer("%VOICES%", filmPlanVoiceGuide(), "%EMOTIONS%", strings.Join(labels, ", ")).Replace(filmPlanRules)
}

func filmPlanPrompt(in *filmPlanJob) string {
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
	b.WriteString("You are a film director and screenwriter. The episode is generated shot by shot by a reference-to-video AI model that receives each element's reference image, so every shot must be self-contained and consistent.\n")
	fmt.Fprintf(&b, "Visual style: %s. Aspect ratio %s.\n", style, in.Ratio)
	shotJSON := `{"scene":1,"seconds":5,"characters":["names"],"location":"name","props":["names"],"action":"English","camera":"English","dialogue":[{"speaker":"name","line":"Thai","emotion":"one listed emotion","delivery":"English, short"}],"audio":"English"}`
	if in.ShotID == "" && len(in.Shots) > 0 {
		b.WriteString("Task: edit the existing episode following the user's request (add, remove, reorder, split or change shots or scenes). Return the complete new ordered shot list. For every existing shot that stays exactly as it is, return only {\"keep\": its no, \"scene\": n} so its finished video is kept; write a full shot only for new or changed shots. Change only what the request needs. Keep shot seconds unless asked; the total length may change. Only if the user clearly asks to start over, return all new shots. If the user names a total length, the sum of all shot seconds (kept shots included) must equal it exactly: add the seconds up before answering.\n")
		total := 0
		for _, sh := range in.Shots {
			total += sh.Seconds
		}
		fmt.Fprintf(&b, "The episode is currently %d seconds long in %d shots.\n", total, len(in.Shots))
		b.WriteString(filmPlanRulesText() + "\n")
		b.WriteString(`Return exactly JSON: {"reply":"Thai, 1-3 sentences telling the user what you changed","title":"Thai, only if the story changed","logline":"","synopsis":"","theme":"","elements":[{"kind":"character|location|prop","name":"Thai","spec":"English, <=600 chars","voice":"preset id, characters only"}],"shots":[{"keep":1,"scene":1},` + shotJSON + `]}` + "\n")
	} else if in.ShotID == "" {
		fmt.Fprintf(&b, "Task: plan the whole episode from the user's request. Total length must be exactly %d seconds (the sum of all shot seconds), so plan about %d shots of 5-12 seconds each; never pad with long static shots.\n", in.TargetSeconds, filmPlanShotCount(in.TargetSeconds))
		b.WriteString(filmPlanRulesText() + "\n")
		b.WriteString(`Return exactly JSON: {"reply":"Thai, 1-3 sentences telling the user what you made","title":"Thai","logline":"Thai","synopsis":"Thai, <=800 chars","theme":"Thai","elements":[{"kind":"character|location|prop","name":"Thai","spec":"English, <=600 chars","voice":"preset id, characters only"}],"shots":[` + shotJSON + `]}` + "\n")
	} else {
		fmt.Fprintf(&b, "Task: rewrite only shot number %d (1-based) following the user's instruction. Keep its seconds unless asked. Use only existing elements, plus new ones in \"elements\" if the instruction needs them. Keep continuity with the neighboring shots.\n", in.ShotIndex+1)
		b.WriteString(filmPlanRulesText() + "\n")
		b.WriteString(`Return exactly JSON: {"reply":"Thai, 1-2 sentences","elements":[],"shot":{"transition":"cut|continue","seconds":5,"characters":["names"],"location":"name","props":["names"],"action":"English","camera":"English","dialogue":[{"speaker":"name","line":"Thai","emotion":"one listed emotion","delivery":"English"}],"audio":"English"}}` + "\n")
	}
	fmt.Fprintf(&b, "Current episode data: %s\n", ctx)
	fmt.Fprintf(&b, "User request: %s\n", in.Prompt)
	return b.String()
}

// filmPlanShotCount suggests how many shots fill a target length at about 8 seconds per shot.
func filmPlanShotCount(target int) int {
	return min(max((target+4)/8, 3), filmMaxShots)
}

// filmElementName drops the @/# markers the planner sometimes copies from the prompt into names.
func filmElementName(name string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(name), "@#＠＃"))
}

// filmBalanceSeconds clamps every shot to 4-15 s and moves the total to target one second at a time.
func filmBalanceSeconds(shots []req.FilmShotRequest, target int) {
	sum := 0
	for i := range shots {
		shots[i].Seconds = min(max(shots[i].Seconds, filmMinSeconds), filmMaxSeconds)
		sum += shots[i].Seconds
	}
	for diff := target - sum; diff != 0; {
		moved := false
		for i := range shots {
			if diff > 0 && shots[i].Seconds < filmMaxSeconds {
				shots[i].Seconds++
				diff--
				moved = true
			} else if diff < 0 && shots[i].Seconds > filmMinSeconds {
				shots[i].Seconds--
				diff++
				moved = true
			}
			if diff == 0 {
				break
			}
		}
		if !moved {
			break
		}
	}
}

// filmResolveShot turns an LLM shot into a valid request, dropping references it cannot resolve.
func filmResolveShot(p filmPlanShot, prevLocation string, first bool, resolve func(kind, name string) string) req.FilmShotRequest {
	out := req.FilmShotRequest{Seconds: p.Seconds, Transition: "cut", CharacterIDs: []string{}, PropIDs: []string{}, Dialogue: []req.FilmLineRequest{}}
	seen := map[string]bool{}
	for _, n := range p.Characters {
		if id := resolve("character", n); id != "" && !seen[id] && len(out.CharacterIDs) < filmMaxCharacters {
			out.CharacterIDs, seen[id] = append(out.CharacterIDs, id), true
		}
	}
	for _, n := range p.Props {
		if id := resolve("prop", n); id != "" && !seen[id] && len(out.PropIDs) < filmMaxProps {
			out.PropIDs, seen[id] = append(out.PropIDs, id), true
		}
	}
	out.LocationID = resolve("location", p.Location)
	if !first && p.Transition == "continue" && out.LocationID == prevLocation {
		out.Transition = "continue"
	}
	out.Action = directorClipRunes(strings.TrimSpace(p.Action), filmActionMax)
	if out.Action == "" {
		out.Action = "Hold on the scene with subtle natural movement."
	}
	out.Camera = directorClipRunes(strings.TrimSpace(p.Camera), filmCameraMax)
	out.Audio = directorClipRunes(strings.TrimSpace(p.Audio), filmAudioMax)
	for _, l := range p.Dialogue {
		id := resolve("character", l.Speaker)
		line := directorClipRunes(strings.TrimSpace(l.Line), filmLineMax)
		if id == "" || !seen[id] || line == "" || len(out.Dialogue) >= filmMaxLines {
			continue
		}
		emotion := strings.TrimSpace(l.Emotion)
		if _, ok := voiceEmotionPrompt(emotion); !ok {
			emotion = ""
		}
		out.Dialogue = append(out.Dialogue, req.FilmLineRequest{SpeakerID: id, Line: line, Delivery: directorClipRunes(strings.TrimSpace(l.Delivery), filmDeliveryMax), Emotion: emotion})
	}
	return out
}

// filmPlanShots turns a planned shot list into shot requests. A "keep" entry reuses the stored
// shot (same id, so its video and draft survive); shots in the same scene continue from the
// previous shot's last frame. It returns how many shots are new or rewritten.
func filmPlanShots(plan []filmPlanShot, ids []string, d *filmDocument, resolve func(kind, name string) string) ([]req.FilmShotRequest, int) {
	shots := []req.FilmShotRequest{}
	used := map[string]bool{}
	changed, prevLocation, prevScene := 0, "", 0
	for _, p := range plan {
		if len(shots) >= filmMaxShots {
			break
		}
		first := len(shots) == 0
		if p.Keep > 0 {
			if p.Keep > len(ids) || used[ids[p.Keep-1]] {
				continue
			}
			_, sh := filmFindShot(d, ids[p.Keep-1])
			if sh == nil {
				continue
			}
			used[sh.ID] = true
			r := filmShotRequest(sh)
			// A kept shot keeps its own transition (its video depends on it) unless it now opens the
			// episode or the plan moved it into a new scene.
			if first || p.Scene != 0 && p.Scene != prevScene {
				r.Transition = "cut"
			}
			shots = append(shots, r)
			prevLocation, prevScene = r.LocationID, p.Scene
			continue
		}
		if p.Scene != 0 {
			p.Transition = "cut"
			if p.Scene == prevScene {
				p.Transition = "continue"
			}
		}
		r := filmResolveShot(p, prevLocation, first, resolve)
		shots = append(shots, r)
		changed++
		prevLocation, prevScene = r.LocationID, p.Scene
	}
	return shots, changed
}

func (f *FilmFacade) planFail(c context.Context, j *models.FilmJob, message string, cause error) error {
	if cause != nil {
		log.Printf("film: plan job=%s failed: %v", j.ID, cause)
	}
	return f.store.CompleteEpisode(c, j, "failed", true, func(ep *models.FilmEpisode) error {
		d, e := filmDecode(ep)
		if e != nil {
			return e
		}
		if d.Plan == nil || d.Plan.JobID != j.ID.String() {
			return nil
		}
		d.Plan.Status, d.Plan.FailureMessage = "failed", &message
		filmAddMessage(&d, filmMessage{Role: "assistant", Text: message + " (คืนแต้มแล้ว)", At: time.Now().UTC()})
		return filmEncode(ep, &d)
	})
}

func (f *FilmFacade) planJob(c context.Context, j *models.FilmJob) error {
	var in filmPlanJob
	if e := json.Unmarshal(j.Snapshot, &in); e != nil || j.ProjectID == nil {
		return f.planFail(c, j, "ข้อมูลงานไม่ถูกต้อง", e)
	}
	if f.options.Analyzer == nil {
		return f.planFail(c, j, "ระบบวางเรื่องยังไม่พร้อมใช้งาน", nil)
	}
	if e := f.store.ProgressEpisode(c, j, func(ep *models.FilmEpisode) error {
		d, e := filmDecode(ep)
		if e != nil {
			return e
		}
		if d.Plan != nil && d.Plan.JobID == j.ID.String() {
			d.Plan.Status = "processing"
		}
		return filmEncode(ep, &d)
	}); e != nil {
		return e
	}
	text, e := f.options.Analyzer.Analyze(c, in.Model, filmPlanPrompt(&in))
	if e != nil {
		return f.planFail(c, j, "ผู้ช่วยวางเรื่องไม่สำเร็จ กรุณาลองใหม่", e)
	}
	var out filmPlanOutput
	if e = directorDecodeJSON(text, &out); e != nil {
		return f.planFail(c, j, "ผู้ช่วยตอบกลับในรูปแบบที่อ่านไม่ได้ กรุณาลองใหม่", e)
	}
	if in.ShotID == "" && len(out.Shots) == 0 || in.ShotID != "" && out.Shot == nil {
		return f.planFail(c, j, "ผู้ช่วยไม่ได้ส่งช็อตกลับมา กรุณาลองใหม่หรือเล่ารายละเอียดเพิ่ม", nil)
	}
	e = f.store.CompletePlan(c, j, func(ep *models.FilmEpisode, elements []models.FilmElement, s *models.DirectorSeries) ([]models.FilmElement, error) {
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		if d.Plan == nil || d.Plan.JobID != j.ID.String() {
			return nil, nil
		}
		names := map[string]string{}
		for i := range elements {
			names[elements[i].Kind+":"+strings.ToLower(elements[i].Name)] = elements[i].ID.String()
		}
		created := []models.FilmElement{}
		for _, pe := range out.Elements {
			name := directorClipRunes(filmElementName(pe.Name), filmNameMax)
			key := pe.Kind + ":" + strings.ToLower(name)
			if filmAssetKinds[pe.Kind] == "" || name == "" || names[key] != "" || len(elements)+len(created) >= filmPlanMaxElements {
				continue
			}
			now := time.Now().UTC()
			el := models.FilmElement{Base: models.Base{ID: uuid.New(), CreatedAt: now, UpdatedAt: now}, UserID: j.UserID, SeriesID: j.SeriesID, Kind: pe.Kind, Name: name, Spec: directorClipRunes(strings.TrimSpace(pe.Spec), filmSpecMax), ImageStatus: "idle", Revision: 1}
			if pe.Kind == "character" {
				el.Voice = filmPlanVoice(pe.Voice)
			}
			created = append(created, el)
			names[key] = el.ID.String()
		}
		all := append(append([]models.FilmElement{}, elements...), created...)
		resolve := func(kind, name string) string {
			if id := names[kind+":"+strings.ToLower(strings.TrimSpace(name))]; id != "" {
				return id
			}
			return names[kind+":"+strings.ToLower(filmElementName(name))]
		}
		var shots []req.FilmShotRequest
		summary := ""
		if in.ShotID == "" {
			editing := len(in.ShotIDs) > 0
			var changed int
			shots, changed = filmPlanShots(out.Shots, in.ShotIDs, &d, resolve)
			if !editing {
				filmBalanceSeconds(shots, in.TargetSeconds)
				total := 0
				for _, sh := range shots {
					total += sh.Seconds
				}
				// Too few shots cannot reach the target even at 15 s each; say so instead of claiming it.
				if total != in.TargetSeconds {
					summary = fmt.Sprintf(" · ได้ความยาวรวม %d:%02d จากที่ตั้งไว้ %d:%02d พิมพ์ให้เพิ่มช็อตได้", total/60, total%60, in.TargetSeconds/60, in.TargetSeconds%60)
				}
			} else {
				kept, total := len(shots)-changed, 0
				for _, sh := range shots {
					total += sh.Seconds
				}
				summary = fmt.Sprintf(" · ความยาวรวม %d:%02d · คงไว้ %d ช็อต", total/60, total%60, kept)
				if changed > 0 {
					summary += fmt.Sprintf(" แก้/เพิ่ม %d ช็อต", changed)
				}
				if removed := len(in.ShotIDs) - kept; removed > 0 {
					summary += fmt.Sprintf(" ลบ %d ช็อต", removed)
				}
			}
			if len(shots) == 0 {
				return nil, fmt.Errorf("plan returned no usable shots")
			}
		} else {
			i, sh := filmFindShot(&d, in.ShotID)
			if sh == nil {
				return nil, fmt.Errorf("target shot removed")
			}
			prev := ""
			if i > 0 {
				prev = d.Shots[i-1].LocationID
			}
			for k := range d.Shots {
				shots = append(shots, filmShotRequest(&d.Shots[k]))
			}
			r := filmResolveShot(*out.Shot, prev, i == 0, resolve)
			r.ID = sh.ID
			r.Seconds = min(max(r.Seconds, filmMinSeconds), filmMaxSeconds)
			shots[i] = r
		}
		valid, e := filmValidateShots(shots, d.Shots, filmElementMap(all), s.Style)
		if e != nil {
			return nil, e
		}
		d.Shots = valid
		// An edit may leave the story fields empty; keep the current story then.
		if in.ShotID == "" && (strings.TrimSpace(out.Title) != "" || strings.TrimSpace(out.Synopsis) != "" || d.Story == nil) {
			next := filmStory{Title: directorClipRunes(out.Title, 120), Logline: directorClipRunes(out.Logline, 600), Synopsis: directorClipRunes(out.Synopsis, 1200), Theme: directorClipRunes(out.Theme, 400)}
			if d.Story != nil {
				if next.Title == "" {
					next.Title = d.Story.Title
				}
				if next.Logline == "" {
					next.Logline = d.Story.Logline
				}
				if next.Synopsis == "" {
					next.Synopsis = d.Story.Synopsis
				}
				if next.Theme == "" {
					next.Theme = d.Story.Theme
				}
			}
			d.Story = &next
		}
		reply := strings.TrimSpace(out.Reply)
		if reply == "" {
			reply = "เรียบร้อยแล้ว"
		}
		reply += summary
		if len(created) > 0 {
			reply += fmt.Sprintf(" · เพิ่มองค์ประกอบใหม่ %d รายการ กดสร้างภาพในแท็บองค์ประกอบก่อนสร้างวิดีโอ", len(created))
		}
		filmAddMessage(&d, filmMessage{Role: "assistant", Text: reply, ShotID: in.ShotID, At: time.Now().UTC()})
		d.Plan = nil
		return created, filmEncode(ep, &d)
	})
	if e != nil {
		return f.planFail(c, j, "จัดช็อตจากคำตอบของผู้ช่วยไม่สำเร็จ กรุณาลองใหม่", e)
	}
	return nil
}
