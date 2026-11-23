package facade_open_api

// Film export (ภาพยนตร์ phase 4): merges the episode's shot clips, in order, into one MP4 with
// optional Thai dialogue subtitles. Uses the shared FFmpeg processor; Director code is not changed.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
)

// The FFmpeg processor merges at most 20 clips per call; longer episodes are merged in batches.
const filmMergeBatch = 20

type filmExport struct {
	Status         string  `json:"status"` // queued | processing | succeeded | failed
	Progress       int     `json:"progress"`
	JobID          string  `json:"jobId"`
	Key            string  `json:"key,omitempty"`
	Source         string  `json:"source"` // hash of the clip keys the export is built from
	ClipCount      int     `json:"clipCount"`
	Skipped        int     `json:"skipped"`
	Subtitles      bool    `json:"subtitles"`
	FailureMessage *string `json:"failureMessage,omitempty"`
}

type filmExportJob struct {
	ClipKeys   []string `json:"clipKeys"`
	Subtitles  []string `json:"subtitles"`
	Resolution string   `json:"resolution"`
	Ratio      string   `json:"ratio"`
	// Trims match ClipKeys; Music is mixed under the whole film.
	Trims []filmClipTrim `json:"trims,omitempty"`
	Music *filmMusic     `json:"music,omitempty"`
	// Dubs match ClipKeys: a clip whose dialogue lines were moved is mixed again from its raw clip.
	Dubs []*filmExportDub `json:"dubs,omitempty"`
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
		keys, subs, skipped := filmExportClips(&d, filmElementMap(elements))
		if len(keys) == 0 {
			return nil, &DirectorError{Status: 409, Code: "VIDEO_INCOMPLETE", Message: "ยังไม่มีช็อตที่สร้างวิดีโอเสร็จ กรุณาสร้างวิดีโออย่างน้อย 1 ช็อตก่อน"}
		}
		if !in.Subtitles {
			subs = nil
		}
		music := d.Music
		if music != nil && f.options.MusicFetch == nil {
			music = nil
		}
		raw, e := json.Marshal(filmExportJob{ClipKeys: keys, Subtitles: subs, Resolution: s.Resolution, Ratio: s.AspectRatio, Trims: filmExportTrims(&d), Music: music, Dubs: filmExportDubs(&d)})
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

// filmTextLayers turns per-clip subtitles into bottom text layers.
func filmTextLayers(subs []string, offset int) []videoprocessing.TextLayer {
	out := []videoprocessing.TextLayer{}
	for i, s := range subs {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, videoprocessing.TextLayer{ClipIdx: i - offset, Text: s, Position: "bottom", FontSize: 40})
		}
	}
	return out
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
	clips := make([]videoprocessing.Clip, 0, len(in.ClipKeys))
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
		if e == nil && (trimmed || in.Music != nil) {
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
	var data []byte
	if len(clips) <= filmMergeBatch {
		data, e = f.options.Processor.Merge(c, videoprocessing.MergeRequest{Clips: clips, OutputResolution: in.Resolution, OutputAspectRatio: in.Ratio, AudioVolume: 1, TextLayers: filmTextLayers(in.Subtitles, 0), AudioTracks: music})
	} else {
		parts := []videoprocessing.Clip{}
		for start := 0; start < len(clips) && e == nil; start += filmMergeBatch {
			end := min(start+filmMergeBatch, len(clips))
			var subs []string
			if len(in.Subtitles) >= end {
				subs = in.Subtitles[:end]
			}
			var part []byte
			part, e = f.options.Processor.Merge(c, videoprocessing.MergeRequest{Clips: clips[start:end], OutputResolution: in.Resolution, OutputAspectRatio: in.Ratio, AudioVolume: 1, TextLayers: filmTextLayersRange(subs, start)})
			if e == nil {
				path := filepath.Join(dir, fmt.Sprintf("part-%02d.mp4", start/filmMergeBatch))
				if e = os.WriteFile(path, part, 0o600); e == nil {
					parts = append(parts, videoprocessing.Clip{Path: path})
				}
			}
		}
		if e == nil {
			data, e = f.options.Processor.Merge(c, videoprocessing.MergeRequest{Clips: parts, OutputResolution: in.Resolution, OutputAspectRatio: in.Ratio, AudioVolume: 1, AudioTracks: music})
		}
	}
	if e != nil || len(data) == 0 {
		return f.exportFail(c, j, "รวมภาพยนตร์ไม่สำเร็จ กรุณาลองใหม่", fmt.Errorf("merge %d clips: %v", len(clips), e))
	}
	if e = progress(90); e != nil {
		return e
	}
	key := fmt.Sprintf("film/%s/%s/export/%s.mp4", j.UserID, *j.ProjectID, j.ID)
	if e = f.storage.Upload(c, key, data, "video/mp4"); e != nil {
		return f.exportFail(c, j, "บันทึกไฟล์ภาพยนตร์ไม่ได้ กรุณาลองใหม่", e)
	}
	return f.exportState(c, j, "succeeded", func(x *filmExport) {
		x.Status, x.Progress, x.Key, x.FailureMessage = "succeeded", 100, key, nil
	})
}

// filmTextLayersRange keeps the subtitles of clips [start, len(subs)) re-indexed to the batch.
func filmTextLayersRange(subs []string, start int) []videoprocessing.TextLayer {
	if len(subs) <= start {
		return nil
	}
	return filmTextLayers(subs[start:], 0)
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
