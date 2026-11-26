package facade_open_api

// Film export (ภาพยนตร์ phase 4): merges the episode's shot clips, in order, into one MP4. The
// dialogue becomes timed Thai subtitles: always stored as .srt and .vtt next to the film, and
// burned into the picture on request. Uses the shared FFmpeg processor; Director code is not changed.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/subtitles"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
)

// The FFmpeg processor merges at most 20 clips per call; longer episodes are merged in batches.
const filmMergeBatch = 20

type filmExport struct {
	Status    string `json:"status"` // queued | processing | succeeded | failed
	Progress  int    `json:"progress"`
	JobID     string `json:"jobId"`
	Key       string `json:"key,omitempty"`
	Source    string `json:"source"` // hash of the clip keys the export is built from
	ClipCount int    `json:"clipCount"`
	Skipped   int    `json:"skipped"`
	Subtitles bool   `json:"subtitles"`
	// Captions: subtitle files were stored next to Key (same name, .srt and .vtt).
	Captions       bool    `json:"captions,omitempty"`
	FailureMessage *string `json:"failureMessage,omitempty"`
}

type filmExportJob struct {
	ClipKeys []string `json:"clipKeys"`
	// Subtitles is the per-clip text of jobs queued before Captions (burned, untimed).
	Subtitles []string `json:"subtitles,omitempty"`
	// Captions match ClipKeys: each clip's dialogue, timed where the lines were generated.
	Captions [][]filmCaption `json:"captions,omitempty"`
	// Burn draws the subtitles into the picture; the subtitle files are stored either way.
	Burn       bool   `json:"burn,omitempty"`
	Resolution string `json:"resolution"`
	Ratio      string `json:"ratio"`
	// Trims match ClipKeys; Music is mixed under the whole film.
	Trims []filmClipTrim `json:"trims,omitempty"`
	Music *filmMusic     `json:"music,omitempty"`
	// Dubs match ClipKeys: a clip whose dialogue lines were moved is mixed again from its raw clip.
	Dubs []*filmExportDub `json:"dubs,omitempty"`
	// Finish grades the merged film (videoprocessing.FinishFilm for live-action styles).
	Finish string `json:"finish,omitempty"`
}

// filmFinish is the post grade an episode's export gets: live-action styles are finished like
// film (print look, halation, grain) so generated clips stop looking generated; drawn styles
// keep their own colours.
func filmFinish(style string) string {
	if filmFilmed(style) {
		return videoprocessing.FinishFilm
	}
	return ""
}

// filmExportClips lists the succeeded clips in shot order and the matching subtitle text.
func filmExportClips(d *filmDocument, byID map[string]*models.FilmElement) ([]string, []string, int) {
	keys, subs, skipped := []string{}, []string{}, 0
	for i := range d.Shots {
		v := d.Shots[i].Video
		if v == nil || v.Status != "succeeded" || v.ClipKey == "" {
			skipped++
			continue
		}
		keys = append(keys, v.ClipKey)
		lines := []string{}
		for _, l := range d.Shots[i].Dialogue {
			lines = append(lines, l.Line)
		}
		subs = append(subs, strings.Join(lines, " "))
	}
	return keys, subs, skipped
}

func filmExportSource(keys []string) string { return storyHash(keys) }

// filmCaption is one dialogue line of an exported clip. Timed lines were generated (dubbed) and
// play at At for Seconds from the clip start; other lines (native speech, clips made before
// lines were kept) only have their text.
type filmCaption struct {
	Text    string  `json:"text"`
	At      float64 `json:"at,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
	Timed   bool    `json:"timed,omitempty"`
}

// filmExportCaptions lists the dialogue of the clips filmExportClips returns, in the same order.
func filmExportCaptions(d *filmDocument) [][]filmCaption {
	out := [][]filmCaption{}
	for i := range d.Shots {
		sh := &d.Shots[i]
		v := sh.Video
		if v == nil || v.Status != "succeeded" || v.ClipKey == "" {
			continue
		}
		lines := []filmCaption{}
		if !v.Native && len(v.Lines) > 0 {
			for _, l := range filmLinePlaces(sh) {
				lines = append(lines, filmCaption{Text: l.Text, At: l.At, Seconds: l.Seconds, Timed: true})
			}
		} else {
			for _, l := range sh.Dialogue {
				lines = append(lines, filmCaption{Text: l.Line})
			}
		}
		out = append(out, lines)
	}
	return out
}

// filmClipSpan places a clip on the film: Offset is where it starts, TrimStart the source second
// it starts from and Kept how long it plays.
type filmClipSpan struct{ Offset, TrimStart, Kept float64 }

// filmCues turns the captions into subtitle cues on the film timeline. A timed line shows while
// it is spoken (cut by the clip's trims); lines without a time share their clip by length after a
// short lead-in. Long lines become several cues of at most two lines (subtitles.Place).
func filmCues(captions [][]filmCaption, spans []filmClipSpan, total float64) []subtitles.Cue {
	cues := []subtitles.Cue{}
	for i, lines := range captions {
		if i >= len(spans) {
			break
		}
		sp := spans[i]
		end := sp.Offset + sp.Kept
		untimed, weight := []filmCaption{}, 0
		for _, l := range lines {
			if strings.TrimSpace(l.Text) == "" {
				continue
			}
			if !l.Timed {
				untimed, weight = append(untimed, l), weight+subtitles.Width(l.Text)+1
				continue
			}
			start := sp.Offset + l.At - sp.TrimStart
			cues = append(cues, subtitles.Place(l.Text, math.Max(start, sp.Offset), math.Min(start+l.Seconds, end))...)
		}
		lead := math.Min(0.3, sp.Kept/10)
		at, room := sp.Offset+lead, sp.Kept-2*lead
		for _, l := range untimed {
			next := at + room*float64(subtitles.Width(l.Text)+1)/float64(weight)
			cues = append(cues, subtitles.Place(l.Text, at, next)...)
			at = next
		}
	}
	return subtitles.Tidy(cues, total)
}

func (f *FilmFacade) RequestExport(c context.Context, projectID, user string, in req.FilmExportRequest) (dto.FilmEpisode, error) {
	if !filmUUID(projectID) {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	if !filmUUID(in.IdempotencyKey) {
		return dto.FilmEpisode{}, directorInvalid("idempotencyKey")
	}
	if f.options.Processor == nil || f.options.VideoFetch == nil || f.storage == nil {
		return dto.FilmEpisode{}, &DirectorError{Status: 503, Code: "EXPORT_UNAVAILABLE", Message: "ยังไม่ได้ตั้งค่าระบบรวมวิดีโอ"}
	}
	hash := storyHash(struct {
		Action, Project string
		Input           req.FilmExportRequest
	}{"film-export", projectID, in})
	_, e := f.store.ChangeEpisode(c, projectID, user, in.IdempotencyKey, hash, func(ep *models.FilmEpisode, _ *models.DirectorProject, s *models.DirectorSeries, elements []models.FilmElement) ([]models.FilmJob, error) {
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		if d.Export != nil && (d.Export.Status == "queued" || d.Export.Status == "processing") {
			return nil, &DirectorError{Status: 409, Code: "EXPORT_BUSY", Message: "กำลังรวมภาพยนตร์อยู่ กรุณารอสักครู่"}
		}
		keys, _, skipped := filmExportClips(&d, filmElementMap(elements))
		if len(keys) == 0 {
			return nil, &DirectorError{Status: 409, Code: "VIDEO_INCOMPLETE", Message: "ยังไม่มีช็อตที่สร้างวิดีโอเสร็จ กรุณาสร้างวิดีโออย่างน้อย 1 ช็อตก่อน"}
		}
		music := d.Music
		if music != nil && f.options.MusicFetch == nil {
			music = nil
		}
		raw, e := json.Marshal(filmExportJob{ClipKeys: keys, Captions: filmExportCaptions(&d), Burn: in.Subtitles, Resolution: s.Resolution, Ratio: s.AspectRatio, Trims: filmExportTrims(&d), Music: music, Dubs: filmExportDubs(&d), Finish: filmFinish(s.Style)})
		if e != nil {
			return nil, e
		}
		jobID := uuid.New()
		d.Export = &filmExport{Status: "queued", JobID: jobID.String(), Source: filmExportSourceOf(&d), ClipCount: len(keys), Skipped: skipped, Subtitles: in.Subtitles}
		if e = filmEncode(ep, &d); e != nil {
			return nil, e
		}
		project := ep.ProjectID
		return []models.FilmJob{{ID: jobID, SeriesID: s.ID, ProjectID: &project, Kind: "export", TargetID: "export:" + projectID, Snapshot: raw, Cost: 0}}, nil
	})
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	return f.GetEpisode(c, projectID, user)
}

func (f *FilmFacade) exportState(c context.Context, j *models.FilmJob, final string, fn func(*filmExport)) error {
	update := func(ep *models.FilmEpisode) error {
		d, e := filmDecode(ep)
		if e != nil {
			return e
		}
		if d.Export == nil || d.Export.JobID != j.ID.String() {
			return nil
		}
		fn(d.Export)
		return filmEncode(ep, &d)
	}
	if final == "" {
		return f.store.ProgressEpisode(c, j, update)
	}
	return f.store.CompleteEpisode(c, j, final, false, update)
}

func (f *FilmFacade) exportFail(c context.Context, j *models.FilmJob, message string, cause error) error {
	if cause != nil {
		log.Printf("film: export job=%s failed: %v", j.ID, cause)
	}
	return f.exportState(c, j, "failed", func(x *filmExport) { x.Status, x.Progress, x.FailureMessage = "failed", 0, &message })
}

func (f *FilmFacade) exportJob(c context.Context, j *models.FilmJob) error {
	var in filmExportJob
	if e := json.Unmarshal(j.Snapshot, &in); e != nil || j.ProjectID == nil || len(in.ClipKeys) == 0 {
		return f.exportFail(c, j, "ข้อมูลงานไม่ถูกต้อง", e)
	}
	if f.options.Processor == nil || f.options.VideoFetch == nil {
		return f.exportFail(c, j, "ยังไม่ได้ตั้งค่าระบบรวมวิดีโอ", nil)
	}
	progress := func(p int) error {
		return f.exportState(c, j, "", func(x *filmExport) { x.Status, x.Progress = "processing", p })
	}
	if e := progress(5); e != nil {
		return e
	}
	dir, e := os.MkdirTemp("", "film-export-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	if in.Captions == nil && len(in.Subtitles) > 0 {
		// Queued before captions: the per-clip text, burned and spread over each clip.
		in.Burn = true
		for _, text := range in.Subtitles {
			in.Captions = append(in.Captions, []filmCaption{{Text: text}})
		}
	}
	captioned := false
	for _, lines := range in.Captions {
		captioned = captioned || len(lines) > 0
	}
	clips := make([]videoprocessing.Clip, 0, len(in.ClipKeys))
	spans := make([]filmClipSpan, 0, len(in.ClipKeys))
	filmLength := 0.0
	for i, key := range in.ClipKeys {
		u, _, e := f.storage.SignedURL(c, key, 30*time.Minute)
		var data []byte
		if e == nil {
			data, _, e = f.options.VideoFetch(c, u)
		}
		path := filepath.Join(dir, fmt.Sprintf("clip-%03d.mp4", i))
		if e == nil && i < len(in.Dubs) && in.Dubs[i] != nil {
			data, e = f.exportRedub(c, in.Dubs[i], dir, i, in)
		}
		if e == nil {
			e = os.WriteFile(path, data, 0o600)
		}
		clip := videoprocessing.Clip{Path: path}
		trimmed := i < len(in.Trims) && (in.Trims[i].In > 0 || in.Trims[i].Out > 0)
		if e == nil && (trimmed || in.Music != nil || captioned) {
			var length float64
			if length, e = videoprocessing.MediaDuration(c, path); e == nil {
				var t filmClipTrim
				if i < len(in.Trims) {
					t = in.Trims[i]
				}
				clip.TrimStart, clip.TrimEnd = filmClipWindow(t, length)
				end := clip.TrimEnd
				if end == 0 {
					end = length
				}
				spans = append(spans, filmClipSpan{Offset: filmLength, TrimStart: clip.TrimStart, Kept: end - clip.TrimStart})
				filmLength += end - clip.TrimStart
			}
		}
		if e != nil {
			return f.exportFail(c, j, "โหลดคลิปสำหรับรวมภาพยนตร์ไม่ได้ กรุณาลองใหม่", fmt.Errorf("clip %d: %w", i+1, e))
		}
		clips = append(clips, clip)
		if e = progress(5 + (i+1)*50/len(in.ClipKeys)); e != nil {
			return e
		}
	}
	music, e := f.exportMusic(c, in.Music, dir, filmLength)
	if e != nil {
		return f.exportFail(c, j, "โหลดเพลงประกอบไม่ได้ กรุณาลองใหม่", e)
	}
	cues := filmCues(in.Captions, spans, filmLength)
	burn := ""
	if len(cues) > 0 && in.Burn {
		burn = filepath.Join(dir, "film.srt")
		if e = os.WriteFile(burn, subtitles.SRT(cues), 0o600); e != nil {
			return e
		}
	}
	// With mastering the music is mixed in the master pass, ducked under dialogue.
	mergeMusic := music
	if f.options.Master != nil {
		mergeMusic = nil
	}
	var data []byte
	if len(clips) <= filmMergeBatch {
		data, e = f.options.Processor.Merge(c, videoprocessing.MergeRequest{Clips: clips, OutputResolution: in.Resolution, OutputAspectRatio: in.Ratio, AudioVolume: 1, AudioTracks: mergeMusic, Finish: in.Finish, Subtitles: burn})
	} else {
		parts := []videoprocessing.Clip{}
		for start := 0; start < len(clips) && e == nil; start += filmMergeBatch {
			end := min(start+filmMergeBatch, len(clips))
			var part []byte
			part, e = f.options.Processor.Merge(c, videoprocessing.MergeRequest{Clips: clips[start:end], OutputResolution: in.Resolution, OutputAspectRatio: in.Ratio, AudioVolume: 1, Finish: in.Finish})
			if e == nil {
				path := filepath.Join(dir, fmt.Sprintf("part-%02d.mp4", start/filmMergeBatch))
				if e = os.WriteFile(path, part, 0o600); e == nil {
					parts = append(parts, videoprocessing.Clip{Path: path})
				}
			}
		}
		if e == nil {
			data, e = f.options.Processor.Merge(c, videoprocessing.MergeRequest{Clips: parts, OutputResolution: in.Resolution, OutputAspectRatio: in.Ratio, AudioVolume: 1, AudioTracks: mergeMusic, Subtitles: burn})
		}
	}
	if e != nil || len(data) == 0 {
		return f.exportFail(c, j, "รวมภาพยนตร์ไม่สำเร็จ กรุณาลองใหม่", fmt.Errorf("merge %d clips: %v", len(clips), e))
	}
	if f.options.Master != nil {
		if e = progress(80); e != nil {
			return e
		}
		if data, e = f.masterExport(c, dir, data, music); e != nil {
			return f.exportFail(c, j, "ปรับระดับเสียงภาพยนตร์ไม่สำเร็จ กรุณาลองใหม่", e)
		}
	}
	if e = progress(90); e != nil {
		return e
	}
	key := fmt.Sprintf("film/%s/%s/export/%s.mp4", j.UserID, *j.ProjectID, j.ID)
	if e = f.storage.Upload(c, key, data, "video/mp4"); e != nil {
		return f.exportFail(c, j, "บันทึกไฟล์ภาพยนตร์ไม่ได้ กรุณาลองใหม่", e)
	}
	if len(cues) > 0 {
		base := strings.TrimSuffix(key, ".mp4")
		if e = f.storage.Upload(c, base+".srt", subtitles.SRT(cues), "application/x-subrip"); e == nil {
			e = f.storage.Upload(c, base+".vtt", subtitles.VTT(cues), "text/vtt")
		}
		if e != nil {
			return f.exportFail(c, j, "บันทึกไฟล์ซับไตเติลไม่ได้ กรุณาลองใหม่", e)
		}
	}
	return f.exportState(c, j, "succeeded", func(x *filmExport) {
		x.Status, x.Progress, x.Key, x.FailureMessage, x.Captions = "succeeded", 100, key, nil, len(cues) > 0
	})
}

// masterExport runs the audio master (Options.Master) over the merged film.
func (f *FilmFacade) masterExport(c context.Context, dir string, merged []byte, music []videoprocessing.AudioTrack) ([]byte, error) {
	src, dst := filepath.Join(dir, "merged.mp4"), filepath.Join(dir, "mastered.mp4")
	if e := os.WriteFile(src, merged, 0o600); e != nil {
		return nil, e
	}
	var bed *videoprocessing.AudioTrack
	if len(music) > 0 {
		bed = &music[0]
	}
	if e := f.options.Master(c, src, dst, bed); e != nil {
		return nil, fmt.Errorf("master: %w", e)
	}
	return os.ReadFile(dst)
}

// exportMusic downloads the music track and places it under the whole film with a short fade-out.
func (f *FilmFacade) exportMusic(c context.Context, m *filmMusic, dir string, length float64) ([]videoprocessing.AudioTrack, error) {
	if m == nil || f.options.MusicFetch == nil || length <= 0 {
		return nil, nil
	}
	u, _, e := f.storage.SignedURL(c, m.Key, 30*time.Minute)
	var data []byte
	if e == nil {
		data, _, e = f.options.MusicFetch(c, u)
	}
	if e != nil {
		return nil, e
	}
	path := filepath.Join(dir, "music"+filepath.Ext(m.Key))
	if e = os.WriteFile(path, data, 0o600); e != nil {
		return nil, e
	}
	seconds, e := videoprocessing.AudioDuration(c, path)
	if e != nil {
		return nil, e
	}
	end := min(seconds, length-m.Start)
	if end < 0.1 {
		return nil, nil
	}
	return []videoprocessing.AudioTrack{{Path: path, TrimEnd: end, At: m.Start, Volume: m.Volume, FadeOut: min(2, end/4)}}, nil
}

// exportRedub mixes a clip's dialogue lines at their moved places over its raw clip.
func (f *FilmFacade) exportRedub(c context.Context, x *filmExportDub, dir string, n int, in filmExportJob) ([]byte, error) {
	if f.options.MusicFetch == nil || f.options.Processor == nil {
		return nil, fmt.Errorf("audio fetch not configured")
	}
	fetch := func(key string, get func(context.Context, string) ([]byte, string, error)) ([]byte, error) {
		u, _, e := f.storage.SignedURL(c, key, 30*time.Minute)
		if e != nil {
			return nil, e
		}
		data, _, e := get(c, u)
		return data, e
	}
	raw, e := fetch(x.RawKey, f.options.VideoFetch)
	if e != nil {
		return nil, fmt.Errorf("raw clip: %w", e)
	}
	rawPath := filepath.Join(dir, fmt.Sprintf("raw-%03d.mp4", n))
	if e = os.WriteFile(rawPath, raw, 0o600); e != nil {
		return nil, e
	}
	length, e := videoprocessing.MediaDuration(c, rawPath)
	if e != nil {
		return nil, e
	}
	lines := []filmDubbed{}
	for k, l := range x.Lines {
		audio, e := fetch(l.Key, f.options.MusicFetch)
		if e != nil {
			return nil, fmt.Errorf("line %d: %w", k, e)
		}
		path := filepath.Join(dir, fmt.Sprintf("line-%03d-%02d.mp3", n, k))
		if e = os.WriteFile(path, audio, 0o600); e != nil {
			return nil, e
		}
		// A line moved near the end is cut at the clip end; one past it is dropped.
		if l.At >= length-0.05 {
			continue
		}
		l.Seconds = min(l.Seconds, length-l.At)
		lines = append(lines, filmDubbed{filmDubLine: l, Path: path})
	}
	if len(lines) == 0 {
		return raw, nil
	}
	return f.mixLines(c, rawPath, lines, in.Resolution, in.Ratio, false)
}
