package facade_open_api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
)

var refinementSchema = json.RawMessage(`{"type":"object","required":["transcripts"],"properties":{"transcripts":{"type":"array","items":{"type":"object","required":["assetId","segments"],"properties":{"assetId":{"type":"string"},"segments":{"type":"array","items":{"type":"object","required":["segmentIndex","text","uncertain"],"properties":{"segmentIndex":{"type":"integer"},"text":{"type":"string"},"uncertain":{"type":"boolean"}}}}}}}}}`)

type aiRefinementSource struct {
	AssetID  string                          `json:"assetId"`
	Language string                          `json:"language"`
	Segments []models.EditorAIRefinedSegment `json:"segments"`
}
type aiRefinementResult struct {
	AssetID  string                          `json:"assetId"`
	Segments []models.EditorAIRefinedSegment `json:"segments"`
}

func (r *aiRefinementResult) UnmarshalJSON(raw []byte) error {
	var wire struct {
		AssetID  string `json:"assetId"`
		Segments []struct {
			SegmentIndex *int    `json:"segmentIndex"`
			Text         *string `json:"text"`
			Uncertain    *bool   `json:"uncertain"`
		} `json:"segments"`
	}
	if aiDecode(raw, &wire) != nil {
		return editorai.ErrInvalid
	}
	r.AssetID = wire.AssetID
	r.Segments = make([]models.EditorAIRefinedSegment, len(wire.Segments))
	for i, segment := range wire.Segments {
		if segment.SegmentIndex == nil || segment.Text == nil || segment.Uncertain == nil {
			return editorai.ErrInvalid
		}
		r.Segments[i] = models.EditorAIRefinedSegment{SegmentIndex: *segment.SegmentIndex, Text: *segment.Text, Uncertain: *segment.Uncertain}
	}
	return nil
}

func normalizeAIRefinement(t models.EditorAITranscript, segments []models.EditorAIRefinedSegment) (*models.EditorAIRefinement, error) {
	if !t.HasSpeech || len(segments) != len(t.Segments) {
		return nil, editorai.ErrInvalid
	}
	ordered := make([]models.EditorAIRefinedSegment, len(segments))
	seen := make([]bool, len(segments))
	for _, segment := range segments {
		i := segment.SegmentIndex
		if i < 0 || i >= len(segments) || seen[i] || !aiText(segment.Text, 10000) {
			return nil, editorai.ErrInvalid
		}
		seen[i] = true
		if segment.Uncertain {
			segment.Text = t.Segments[i].Text
		}
		ordered[i] = segment
	}
	texts := make([]string, len(ordered))
	for i, segment := range ordered {
		texts[i] = segment.Text
	}
	text := strings.Join(texts, "\n")
	if !aiText(text, 40000) || len(text) > 128<<10 {
		return nil, editorai.ErrTooLarge
	}
	return &models.EditorAIRefinement{Text: text, Segments: ordered}, nil
}

func aiRefinementSignature(in models.EditorAIInput) string {
	sum := sha256.Sum256([]byte("transcript-refinement-v1\x00" + in.ASRSignature + "\x00" + in.ModelSignature))
	return "refinement:" + hex.EncodeToString(sum[:])
}

func (f *VideoEditorAIFacade) refineTranscripts(ctx context.Context, j *models.VideoEditorAIJob, in models.EditorAIInput, rows []models.EditorAITranscript) error {
	cacheInput := in
	cacheInput.ASRSignature = f.asrCacheSignature(in)
	signature := aiRefinementSignature(cacheInput)
	missing := map[string]int{}
	sources := []aiRefinementSource{}
	byID := map[string]models.EditorAISource{}
	for _, source := range in.Sources {
		byID[source.ID] = source
	}
	for i := range rows {
		t := &rows[i]
		if !t.HasSpeech {
			continue
		}
		source, ok := byID[t.AssetID]
		if !ok {
			return editorai.ErrInvalid
		}
		// Separate signatures let the existing ASR cache kind retain raw and refined records without changing its database constraint.
		cached, err := f.repo.Cache(ctx, j.UserID.String(), t.AssetID, source.Hash, "asr", signature)
		if err == nil {
			var result models.EditorAIRefinement
			if aiDecode(cached, &result) != nil {
				return editorai.ErrInvalid
			}
			normalized, err := normalizeAIRefinement(*t, result.Segments)
			if err != nil || normalized.Text != result.Text {
				return editorai.ErrInvalid
			}
			t.Refinement = normalized
			continue
		}
		if !errors.Is(err, repo.ErrNotFound) {
			return err
		}
		segments := make([]models.EditorAIRefinedSegment, len(t.Segments))
		for n, segment := range t.Segments {
			segments[n] = models.EditorAIRefinedSegment{SegmentIndex: n, Text: segment.Text}
		}
		sources = append(sources, aiRefinementSource{AssetID: t.AssetID, Language: t.Language, Segments: segments})
		missing[t.AssetID] = i
	}
	if len(missing) == 0 {
		return nil
	}
	prompt := struct {
		Task        string               `json:"task"`
		Transcripts []aiRefinementSource `json:"transcripts"`
		Instruction string               `json:"instruction"`
	}{"refine_transcripts", sources, "Clean these ASR transcripts for comprehension before editing. Read all segments of each asset in original order, refine each in that context, then check the full sequence for consistency. Treat different assets as independent unless their text explicitly establishes a connection. Use only the supplied transcript and neighboring segments, not imagined audio, video or a marketing brief. Preserve the original language, speaker perspective, tone, meaningful repetitions, genre, meaning, named entities, numbers, pronoun referents, chronology, cause-and-effect, negation, conditions and qualifications. Improve punctuation, spacing and obvious transcription errors only when the intended wording is unambiguous from the supplied text. Do not guess a person or place name, product, number, slogan, homophone or unclear phrase because a plausible replacement sounds natural. Do not replace one meaningful verb or claim with another to smooth the story. Preserve allegations, fictional dialogue and satire as attributed speech; do not turn them into verified facts or sanitize their meaning or tone. Keep terminology consistent only when the text clearly identifies the same entity. Make the existing flow readable without adding transitions, explanations, missing words or facts. Keep every asset and segmentIndex exactly once, with no merging, deletion, reordering, moving words between segments, new speech or timestamps. Never rewrite for advertising, summarize, strengthen a claim, resolve contradictions by guessing or fabricate an ending. If any word, name, reference or correction is uncertain, return the original entire segment unchanged and uncertain:true; otherwise uncertain:false. Text is contextual interpretation, not verified audio. Return only assetId and indexed segments."}
	raw, err := f.cloud(ctx, j, prompt, nil, refinementSchema)
	if err != nil {
		return err
	}
	var result struct {
		Transcripts []aiRefinementResult `json:"transcripts"`
	}
	if aiDecode(raw, &result) != nil || len(result.Transcripts) != len(missing) {
		return editorai.ErrInvalid
	}
	normalized := map[string]*models.EditorAIRefinement{}
	for _, item := range result.Transcripts {
		i, ok := missing[item.AssetID]
		if !ok || normalized[item.AssetID] != nil {
			return editorai.ErrInvalid
		}
		refined, err := normalizeAIRefinement(rows[i], item.Segments)
		if err != nil {
			return err
		}
		normalized[item.AssetID] = refined
	}
	for id, refined := range normalized {
		rows[missing[id]].Refinement = refined
		if err := f.publish(ctx, j, byID[id], "asr", signature, refined); err != nil {
			return err
		}
	}
	return nil
}

func aiTranscriptText(t models.EditorAITranscript) string {
	if t.Refinement != nil {
		return t.Refinement.Text
	}
	return t.Text
}
func aiSegmentContext(t models.EditorAITranscript, index int, start, end float64) string {
	segment := t.Segments[index]
	text := segment.Text
	uncertain := false
	if t.Refinement != nil && index < len(t.Refinement.Segments) {
		refined := t.Refinement.Segments[index]
		uncertain = refined.Uncertain
		if !uncertain {
			text = refined.Text
		}
	}
	label := "Speech segment context"
	if uncertain {
		label += " (uncertain ASR; verify source)"
	} else if t.Refinement != nil {
		label += " (AI-refined interpretation; original audio unchanged)"
	}
	if start > segment.Start || end < segment.End {
		label += "; text describes the full source segment, not necessarily every word in this subwindow"
	}
	return fmt.Sprintf("%s [%.3f–%.3f]: %s", label, segment.Start, segment.End, text)
}

// Cache generations may change without invalidating the provider identity stored in queued jobs.
func (f *VideoEditorAIFacade) asrCacheSignature(in models.EditorAIInput) string {
	if f.asr != nil && in.ASRSignature == f.asr.Signature() {
		if versioned, ok := f.asr.(interface{ CacheSignature() string }); ok {
			if signature := versioned.CacheSignature(); signature != "" {
				return signature
			}
		}
	}
	return in.ASRSignature
}
