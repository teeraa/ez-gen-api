package facade_open_api

import (
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
)

// ASR segment ends may exceed the probed source duration by up to 0.1 s (accepted by
// validateAITranscript); evidence built from them must still pass planning preflight.
func TestAIEvidenceClampedToSourceDuration(t *testing.T) {
	t.Parallel()
	asset := uuid.NewString()
	in := models.EditorAIInput{Sources: []models.EditorAISource{{ID: asset, Duration: 10, HasAudio: true}}, TargetDuration: 30, RemoveSilence: true}
	transcript := models.EditorAITranscript{AssetID: asset, HasSpeech: true, Text: "a b", Segments: []models.EditorAISpeech{{Start: 0, End: 5, Text: "a"}, {Start: 5.1, End: 10.08, Text: "b"}}}
	if err := validateAITranscript(transcript, in.Sources[0]); err != nil {
		t.Fatalf("transcript should be accepted: %v", err)
	}
	evidence := buildAIEvidence(in, []models.EditorAITranscript{transcript}, nil)
	if err := validateAICapacity(evidence, in); err != nil {
		t.Fatalf("evidence from an accepted transcript fails planning preflight: %v %+v", err, evidence)
	}
	for _, window := range evidence {
		if window.End > 10 {
			t.Fatalf("window past source end %+v", window)
		}
	}
}

// Two accepted transcript segments may share timestamps; composition must still
// produce unique layer IDs instead of failing the whole plan.
func TestAICompositionUniqueIDsForRepeatedTimestamps(t *testing.T) {
	t.Parallel()
	job, asset := uuid.NewString(), uuid.NewString()
	in := models.EditorAIInput{AutoCompose: true, Sources: []models.EditorAISource{{ID: asset, Duration: 10}}, TargetDuration: 10}
	transcript := models.EditorAITranscript{AssetID: asset, HasSpeech: true, Text: "x", Segments: []models.EditorAISpeech{{Start: 1.95, End: 2, Text: "ซ้ำ"}, {Start: 1.95, End: 2, Text: "ซ้ำอีก"}}}
	if err := validateAITranscript(transcript, in.Sources[0]); err != nil {
		t.Fatalf("transcript should be accepted: %v", err)
	}
	d := models.EditorAIDraft{Segments: []models.EditorAIDraftSegment{{ID: uuid.NewString(), AssetID: asset, Start: 0, End: 4, Volume: 1, Title: "t"}}}
	if err := validateAIDraft(&d, in, nil); err != nil {
		t.Fatal(err)
	}
	composeAIDraft(&d, in, job, []models.EditorAITranscript{transcript})
	if err := validateAIComposition(d, in, job); err != nil {
		t.Fatalf("composition rejected: %v %+v", err, d.TextLayers)
	}
}
