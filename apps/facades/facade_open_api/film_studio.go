package facade_open_api

// Film studio edits: trims at a shot clip's start/end and one music track under the whole film.
// Neither touches a clip itself, so neither makes a video stale; both change the export.

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
)

const (
	// FilmMusicMaxBytes bounds an uploaded music file.
	FilmMusicMaxBytes   = 30 << 20
	filmMusicMaxSeconds = 20 * 60
	filmMusicVolume     = 0.35
	// A trimmed clip keeps at least this many seconds.
	filmMinKept = 1.0
)

type filmMusic struct {
	Key     string  `json:"key"`
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	Volume  float64 `json:"volume"`
}

type filmClipTrim struct {
	In  float64 `json:"in,omitempty"`
	Out float64 `json:"out,omitempty"`
}

// filmTrim validates and rounds a shot's trims to 0.1 s; at least filmMinKept seconds remain.
func filmTrim(in, out float64, seconds int) (float64, float64, bool) {
	if math.IsNaN(in) || math.IsNaN(out) || math.IsInf(in, 0) || math.IsInf(out, 0) || in < 0 || out < 0 {
		return 0, 0, false
	}
	in, out = math.Round(in*10)/10, math.Round(out*10)/10
	if in+out > float64(seconds)-filmMinKept+1e-9 {
		return 0, 0, false
	}
	return in, out, true
}

// filmExportTrims lists the trims of the clips filmExportClips returns, in the same order.
func filmExportTrims(d *filmDocument) []filmClipTrim {
	out := []filmClipTrim{}
	for i := range d.Shots {
		v := d.Shots[i].Video
		if v == nil || v.Status != "succeeded" || v.ClipKey == "" {
			continue
		}
		out = append(out, filmClipTrim{In: d.Shots[i].TrimIn, Out: d.Shots[i].TrimOut})
	}
	return out
}

// filmExportSourceOf identifies what an export is built from: the clips, plus trims and music once
// either is used (an episode with neither keeps the hash older exports were stored with).
func filmExportSourceOf(d *filmDocument) string {
	keys, _, _ := filmExportClips(d, nil)
	trims := filmExportTrims(d)
	trimmed := false
	for _, t := range trims {
		trimmed = trimmed || t.In > 0 || t.Out > 0
	}
	if !trimmed && d.Music == nil {
		return filmExportSource(keys)
	}
	return storyHash(struct {
		Keys  []string
		Trims []filmClipTrim
		Music *filmMusic
	}{keys, trims, d.Music})
}

// filmClipWindow turns a shot's trims into the processor's trim window for a clip of `duration`.
func filmClipWindow(t filmClipTrim, duration float64) (float64, float64) {
	if t.In <= 0 && t.Out <= 0 {
		return 0, 0
	}
	start := math.Min(t.In, math.Max(0, duration-filmMinKept))
	end := duration - t.Out
	if end < start+filmMinKept/2 {
		end = math.Min(duration, start+filmMinKept/2)
	}
	if t.Out <= 0 {
		end = 0
	}
	return start, end
}

func (f *FilmFacade) musicAvailable() bool {
	return f.storage != nil && f.options.MusicFetch != nil
}

// SetMusic stores an uploaded audio file as the episode's music track (replacing any previous one).
func (f *FilmFacade) SetMusic(c context.Context, projectID, user, key, name string, data []byte) (dto.FilmEpisode, error) {
	if !filmUUID(projectID) {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	if !filmUUID(key) {
		return dto.FilmEpisode{}, directorInvalid("idempotencyKey")
	}
	if !f.musicAvailable() {
		return dto.FilmEpisode{}, &DirectorError{Status: 503, Code: "MUSIC_UNAVAILABLE", Message: "ยังไม่ได้ตั้งค่าระบบเพลงประกอบ"}
	}
	if len(data) == 0 || len(data) > FilmMusicMaxBytes {
		return dto.FilmEpisode{}, directorInvalid("file")
	}
	ext, kind := filmAudioKind(data)
	if ext == "" {
		return dto.FilmEpisode{}, &DirectorError{Status: 422, Code: "VALIDATION_ERROR", Message: "รองรับไฟล์เพลง MP3, WAV หรือ M4A เท่านั้น"}
	}
	if _, _, _, e := f.store.Episode(c, projectID, user); e != nil {
		return dto.FilmEpisode{}, e
	}
	name = strings.Join(strings.Fields(strings.TrimSuffix(name, filepath.Ext(name))), " ")
	if name == "" || !utf8.ValidString(name) {
		name = "เพลงประกอบ"
	}
	name = directorClipRunes(name, 80)
	seconds, e := filmAudioSeconds(c, data, ext)
	if e != nil || seconds > filmMusicMaxSeconds {
		return dto.FilmEpisode{}, &DirectorError{Status: 422, Code: "VALIDATION_ERROR", Message: "อ่านไฟล์เพลงไม่ได้ หรือยาวเกิน 20 นาที"}
	}
	hash := storyHash(struct {
		Action, Project, Name string
		Size                  int
	}{"film-music", projectID, name, len(data)})
	storeKey := fmt.Sprintf("film/%s/%s/music/%s%s", user, projectID, uuid.NewString(), ext)
	if e = f.storage.Upload(c, storeKey, data, kind); e != nil {
		return dto.FilmEpisode{}, e
	}
	_, e = f.store.ChangeEpisode(c, projectID, user, key, hash, func(ep *models.FilmEpisode, _ *models.DirectorProject, _ *models.DirectorSeries, _ []models.FilmElement) ([]models.FilmJob, error) {
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		volume := filmMusicVolume
		if d.Music != nil {
			volume = d.Music.Volume
		}
		d.Music = &filmMusic{Key: storeKey, Name: name, Seconds: math.Round(seconds*10) / 10, Volume: volume}
		return nil, filmEncode(ep, &d)
	})
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	return f.GetEpisode(c, projectID, user)
}

// UpdateMusic changes the music volume or removes the track.
func (f *FilmFacade) UpdateMusic(c context.Context, projectID, user string, in req.FilmMusicRequest) (dto.FilmEpisode, error) {
	if !filmUUID(projectID) {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	if !filmUUID(in.IdempotencyKey) {
		return dto.FilmEpisode{}, directorInvalid("idempotencyKey")
	}
	if !in.Remove && (in.Volume == nil || math.IsNaN(*in.Volume) || *in.Volume < 0 || *in.Volume > 1) {
		return dto.FilmEpisode{}, directorInvalid("volume")
	}
	hash := storyHash(struct {
		Action, Project string
		Input           req.FilmMusicRequest
	}{"film-music-update", projectID, in})
	_, e := f.store.ChangeEpisode(c, projectID, user, in.IdempotencyKey, hash, func(ep *models.FilmEpisode, _ *models.DirectorProject, _ *models.DirectorSeries, _ []models.FilmElement) ([]models.FilmJob, error) {
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		if d.Music == nil {
			return nil, &DirectorError{Status: 409, Code: "NO_MUSIC", Message: "ตอนนี้ยังไม่มีเพลงประกอบ"}
		}
		if in.Remove {
			d.Music = nil
		} else {
			d.Music.Volume = math.Round(*in.Volume*100) / 100
		}
		return nil, filmEncode(ep, &d)
	})
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	return f.GetEpisode(c, projectID, user)
}

// filmAudioKind accepts MP3, WAV and M4A/AAC; it returns the file extension and content type.
func filmAudioKind(data []byte) (string, string) {
	if len(data) > 8 && string(data[4:8]) == "ftyp" {
		return ".m4a", "audio/mp4"
	}
	switch kind := http.DetectContentType(data); {
	case kind == "audio/mpeg":
		return ".mp3", kind
	case kind == "audio/wave" || kind == "audio/wav" || kind == "audio/x-wav":
		return ".wav", "audio/wav"
	case kind == "audio/aac":
		return ".aac", kind
	}
	return "", ""
}

func filmAudioSeconds(c context.Context, data []byte, ext string) (float64, error) {
	dir, e := os.MkdirTemp("", "film-music-*")
	if e != nil {
		return 0, e
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "music"+ext)
	if e = os.WriteFile(path, data, 0o600); e != nil {
		return 0, e
	}
	return videoprocessing.AudioDuration(c, path)
}
