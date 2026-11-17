package facade_open_api

import (
	"context"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/google/uuid"
)

// Captions for cuts that cross a word-less speech segment (Typhoon contexts have no word
// times, so nothing says which words lie inside the cut). Instead of guessing, the part
// of the audio inside the cut is transcribed again on its own: that text was spoken
// inside the cut by construction. Its timing inside the window is still estimated.

const (
	aiWindowMinSeconds   = .6  // shorter windows rarely hold a whole word
	aiWindowMaxAudio     = 300 // seconds of re-transcribed audio per run
	aiWindowBatch        = 8   // the local ASR runners accept at most 8 files per call
	aiStoryWindowLimit   = 12  // per story-job composition
	aiChatWindowLimit    = 6   // per chat add_subtitles (it runs inside a 140 s reply)
	aiPauseSnapSeconds   = .5  // how far a chat cut may move to land on a pause
	aiPauseSnapMinLength = .3
)

type aiASRWindow struct {
	CutID   string
	AssetID string
	Start   float64
	End     float64
}

// aiWordless reports a speech segment whose words carry no usable timing.
func aiWordless(s models.EditorAISpeech) bool {
	if len(s.Words) == 0 {
		return true
	}
	for _, w := range s.Words {
		if w.End <= w.Start {
			return true
		}
	}
	return false
}

// aiPartialSpeechWindows lists the parts of word-less speech segments that the cut
// crosses (segments fully inside the cut are captioned from the transcript already).
func aiPartialSpeechWindows(t models.EditorAITranscript, cut models.EditorAIDraftSegment) []aiASRWindow {
	out := []aiASRWindow{}
	for _, s := range t.Segments {
		if s.End <= cut.Start || s.Start >= cut.End || !aiWordless(s) || strings.TrimSpace(s.Text) == "" {
			continue
		}
		if s.Start >= cut.Start-1e-6 && s.End <= cut.End+1e-6 {
			continue
		}
		start, end := math.Max(s.Start, cut.Start), math.Min(s.End, cut.End)
		if end-start >= aiWindowMinSeconds {
			out = append(out, aiASRWindow{CutID: cut.ID, AssetID: cut.AssetID, Start: start, End: end})
		}
	}
	return out
}

// partialCaptions re-transcribes the crossed parts of word-less speech for each cut and
// returns, per cut ID, speech segments that lie inside that cut. Best effort: any
// failure (no ASR, download, ffmpeg, ASR error) just leaves those parts uncaptioned.
func (f *VideoEditorAIFacade) partialCaptions(ctx context.Context, sources map[string]models.EditorAISource, cuts []models.EditorAIDraftSegment, transcripts map[string]models.EditorAITranscript, limit int) (map[string][]models.EditorAISpeech, int) {
	out := map[string][]models.EditorAISpeech{}
	windows, audio := []aiASRWindow{}, 0.0
	for _, cut := range cuts {
		t, ok := transcripts[cut.AssetID]
		if !ok {
			continue
		}
		for _, w := range aiPartialSpeechWindows(t, cut) {
			if len(windows) < limit && audio+(w.End-w.Start) <= aiWindowMaxAudio {
				windows = append(windows, w)
				audio += w.End - w.Start
			}
		}
	}
	if len(windows) == 0 || f == nil || f.asr == nil || !f.asr.Available() {
		return out, 0
	}
	dir, e := os.MkdirTemp("", "editor-ai-windows-*")
	if e != nil {
		return out, 0
	}
	defer os.RemoveAll(dir)
	paths := map[string]string{}
	pending, byID := []editorai.Audio{}, map[string]aiASRWindow{}
	run := func() {
		if len(pending) == 0 {
			return
		}
		rows, err := f.asr.Transcribe(ctx, pending)
		pending = pending[:0]
		if err != nil {
			log.Printf("video_editor_ai: caption windows transcription failed: %v", err)
			return
		}
		for _, row := range rows {
			w, ok := byID[row.AssetID]
			if !ok {
				continue
			}
			for _, s := range row.Segments {
				text := strings.TrimSpace(s.Text)
				start, end := math.Max(w.Start, w.Start+s.Start), math.Min(w.End, w.Start+s.End)
				if text == "" || !aiFinite(start) || !aiFinite(end) || end-start < .2 {
					continue
				}
				out[w.CutID] = append(out[w.CutID], models.EditorAISpeech{Start: start, End: end, Text: text})
			}
		}
	}
	for _, w := range windows {
		source, ok := sources[w.AssetID]
		if !ok || !source.HasAudio || source.Key == "" {
			continue
		}
		path := paths[w.AssetID]
		if path == "" {
			if path, e = f.sourceFile(ctx, source, dir); e != nil {
				continue
			}
			paths[w.AssetID] = path
		}
		id := uuid.NewString()
		wav := filepath.Join(dir, id+".wav")
		if aiCommand(ctx, "-v", "error", "-ss", strconv.FormatFloat(w.Start, 'f', 3, 64), "-t", strconv.FormatFloat(w.End-w.Start, 'f', 3, 64), "-i", path, "-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-y", wav) != nil {
			continue
		}
		byID[id] = w
		pending = append(pending, editorai.Audio{AssetID: id, Path: wav, Duration: w.End - w.Start})
		if len(pending) == aiWindowBatch {
			run()
		}
	}
	run()
	return out, len(windows)
}

// aiSnapToPauses moves a cut's start/end that falls inside a speech segment to that
// segment's nearer boundary when it is within limit seconds, so the cut does not split
// an utterance. Boundaries of pause-derived segments are quiet points.
func aiSnapToPauses(t models.EditorAITranscript, start, end, limit float64) (float64, float64, int) {
	snap := func(at float64) (float64, bool) {
		for _, s := range t.Segments {
			if s.Start < at-1e-6 && at < s.End-1e-6 {
				before, after := at-s.Start, s.End-at
				switch {
				case before <= after && before <= limit:
					return s.Start, true
				case after < before && after <= limit:
					return s.End, true
				}
				return at, false
			}
		}
		return at, false
	}
	ns, movedStart := snap(start)
	ne, movedEnd := snap(end)
	if ne-ns < aiPauseSnapMinLength {
		return start, end, 0
	}
	moved := 0
	if movedStart {
		moved++
	}
	if movedEnd {
		moved++
	}
	return ns, ne, moved
}
