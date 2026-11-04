package main

import (
	"context"
	"encoding/json"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/gofiber/fiber/v2"
	"sync"
	"time"
)

type fixtureASR struct{}

func (*fixtureASR) Available() bool   { return true }
func (*fixtureASR) Signature() string { return "fixture-only-ASR-v1" }
func (*fixtureASR) Transcribe(ctx context.Context, audio []editorai.Audio) ([]models.EditorAITranscript, error) {
	out := []models.EditorAITranscript{}
	for _, a := range audio {
		out = append(out, models.EditorAITranscript{AssetID: a.AssetID, Language: "th", Text: "คำพูดทดสอบ", HasSpeech: true, Segments: []models.EditorAISpeech{{Start: 0, End: a.Duration, Text: "คำพูดทดสอบ", Words: []models.EditorAIWord{{Start: 0, End: a.Duration, Text: "คำพูดทดสอบ"}}}}})
	}
	return out, nil
}

type fixtureMode struct {
	AssetID   string `json:"assetId"`
	Mode      string `json:"mode"`
	Remaining int    `json:"remaining"`
}
type fixtureControl struct {
	mu    sync.Mutex
	modes map[string]fixtureMode
	calls map[string]int
}

func newFixtureControl() *fixtureControl {
	return &fixtureControl{modes: map[string]fixtureMode{}, calls: map[string]int{}}
}
func (f *fixtureControl) control(c *fiber.Ctx) error {
	if c.Get("Authorization") != "Bearer video-editor-fixture" {
		return c.SendStatus(401)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.Method() == "POST" {
		var v fixtureMode
		if c.BodyParser(&v) != nil {
			return c.SendStatus(422)
		}
		if v.Mode != "normal" && v.Mode != "quota" && v.Mode != "invalid" && v.Mode != "slow" {
			return c.SendStatus(422)
		}
		if v.Remaining < 1 {
			v.Remaining = 1
		}
		f.modes[v.AssetID] = v
	}
	return c.JSON(struct {
		Modes map[string]fixtureMode `json:"modes"`
		Calls map[string]int         `json:"calls"`
	}{f.modes, f.calls})
}
func (f *fixtureControl) generate(c *fiber.Ctx) error {
	if c.Get("x-goog-api-key") != "fixture-only" {
		return c.SendStatus(401)
	}
	var request struct {
		Contents []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		Generation struct {
			ResponseFormat struct {
				Text struct {
					MIME   string          `json:"mimeType"`
					Schema json.RawMessage `json:"schema"`
				} `json:"text"`
			} `json:"responseFormat"`
		} `json:"generationConfig"`
	}
	if c.BodyParser(&request) != nil || len(request.Contents) != 1 || len(request.Contents[0].Parts) == 0 || request.Generation.ResponseFormat.Text.MIME != "APPLICATION_JSON" || !json.Valid(request.Generation.ResponseFormat.Text.Schema) {
		return c.SendStatus(422)
	}
	var prompt struct {
		Task    string  `json:"task"`
		Target  float64 `json:"targetDuration"`
		Sources []struct {
			AssetID string    `json:"assetId"`
			Times   []float64 `json:"times"`
		} `json:"sources"`
		Transcripts []struct {
			AssetID  string                          `json:"assetId"`
			Segments []models.EditorAIRefinedSegment `json:"segments"`
		} `json:"transcripts"`
		Evidence []models.EditorAIEvidence `json:"evidence"`
	}
	if json.Unmarshal([]byte(request.Contents[0].Parts[0].Text), &prompt) != nil {
		return c.SendStatus(422)
	}
	ids := []string{}
	for _, s := range prompt.Sources {
		ids = append(ids, s.AssetID)
	}
	for _, s := range prompt.Transcripts {
		ids = append(ids, s.AssetID)
	}
	for _, e := range prompt.Evidence {
		ids = append(ids, e.AssetID)
	}
	mode := "normal"
	f.mu.Lock()
	f.calls[prompt.Task]++
	for _, id := range ids {
		v, ok := f.modes[id]
		if ok && v.Remaining > 0 && v.Mode != "normal" {
			mode = v.Mode
			v.Remaining--
			f.modes[id] = v
			break
		}
	}
	f.mu.Unlock()
	if mode == "quota" {
		c.Set("Retry-After", "1")
		return c.Status(429).JSON(fiber.Map{"error": "fixture quota"})
	}
	if mode == "slow" {
		time.Sleep(3 * time.Second)
	}
	var result any
	switch prompt.Task {
	case "refine_transcripts":
		result = struct {
			Transcripts any `json:"transcripts"`
		}{prompt.Transcripts}
	case "vision":
		visuals := []models.EditorAIVisual{}
		for _, s := range prompt.Sources {
			frames := []models.EditorAIVisualFrame{}
			for _, at := range s.Times {
				frames = append(frames, models.EditorAIVisualFrame{Time: at, Description: "ภาพจากฟุตเทจทดสอบ"})
			}
			visuals = append(visuals, models.EditorAIVisual{AssetID: s.AssetID, Summary: "คลิปทดสอบระบบวิเคราะห์", Frames: frames})
		}
		result = struct {
			Visuals []models.EditorAIVisual `json:"visuals"`
		}{visuals}
	case "concepts":
		result = struct {
			Concepts []struct {
				Title    string  `json:"title"`
				Summary  string  `json:"summary"`
				Duration float64 `json:"duration"`
			} `json:"concepts"`
		}{Concepts: []struct {
			Title    string  `json:"title"`
			Summary  string  `json:"summary"`
			Duration float64 `json:"duration"`
		}{{"เปิดด้วยภาพเด่น", "ใช้ภาพและคำพูดที่มีอยู่ในคลิป", prompt.Target}, {"เล่าเรื่องตามลำดับ", "เรียงเรื่องจากฟุตเทจที่มี", prompt.Target}}}
	case "planning":
		type segment struct {
			EvidenceID string `json:"evidenceId"`
			Title      string `json:"title"`
			Reason     string `json:"reason"`
		}
		segments := []segment{}
		duration := 0.0
		chosen := []models.EditorAIEvidence{}
		for _, e := range prompt.Evidence {
			overlap := false
			for _, old := range chosen {
				if old.AssetID == e.AssetID && old.Start < e.End && e.Start < old.End {
					overlap = true
				}
			}
			if overlap {
				continue
			}
			if len(segments) >= 20 || duration+e.End-e.Start > prompt.Target {
				continue
			}
			chosen = append(chosen, e)
			segments = append(segments, segment{e.ID, "ฉากจากคลิปต้นฉบับ", "ช่วงนี้อ้างอิงภาพหรือคำพูดจริง"})
			duration += e.End - e.Start
		}
		result = struct {
			Segments []segment `json:"segments"`
		}{segments}
	default:
		return c.SendStatus(422)
	}
	raw, _ := json.Marshal(result)
	if mode == "invalid" {
		raw = []byte(`{"unexpected":"invalid fixture result"}`)
	}
	return c.JSON(fiber.Map{"candidates": []fiber.Map{{"finishReason": "STOP", "content": fiber.Map{"parts": []fiber.Map{{"text": string(raw)}}}}}})
}
