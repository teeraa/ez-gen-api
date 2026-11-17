package facade_open_api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
)

func aiPlanMaxSegments(evidence []models.EditorAIEvidence, target float64) int {
	lengths := make([]float64, 0, len(evidence))
	for _, window := range evidence {
		lengths = append(lengths, window.End-window.Start)
	}
	sort.Float64s(lengths)
	total, count := 0.0, 0
	// Ignoring overlap gives an upper bound: no feasible selection can contain more windows.
	for _, length := range lengths {
		if count == 20 || total+length > target+.05 {
			break
		}
		total += length
		count++
	}
	if count == 0 {
		return 1
	}
	return count
}

func aiPlanSchema(evidence []models.EditorAIEvidence, target float64) json.RawMessage {
	ids := make([]string, 0, len(evidence))
	for _, window := range evidence {
		ids = append(ids, window.ID)
	}
	var schema map[string]any
	_ = json.Unmarshal(planSchemaTemplate, &schema)
	segments := schema["properties"].(map[string]any)["segments"].(map[string]any)
	segments["maxItems"] = aiPlanMaxSegments(evidence, target)
	properties := segments["items"].(map[string]any)["properties"].(map[string]any)
	properties["evidenceId"] = map[string]any{"type": "string", "enum": ids}
	raw, _ := json.Marshal(schema)
	return raw
}

func aiPlanTimingInstruction(evidence []models.EditorAIEvidence, target float64) string {
	var out strings.Builder
	fmt.Fprintf(&out, " Timing constraints calculated by the server: at most %d selections can fit this target. This is an upper bound, not a required count; overlap and the total duration must still be checked. If this limit is 1, select one complete supplied context, even if the concept describes multiple beats. Do not select a second context to complete a concept at the cost of exceeding the target. Candidate durations in seconds (evidenceId=duration):", aiPlanMaxSegments(evidence, target))
	for _, window := range evidence {
		fmt.Fprintf(&out, " %s=%.6f;", window.ID, window.End-window.Start)
	}
	return out.String()
}

// aiPlanError keeps which planning check failed, so the job's message can say it.
type aiPlanError struct{ category string }

func (e *aiPlanError) Error() string { return "planning " + e.category }
func (e *aiPlanError) Unwrap() error { return editorai.ErrInvalid }

func aiInvalidPlan(jobID, category string) error {
	log.Printf("video_editor_ai job=%s phase=planning validation=%s", jobID, category)
	return &aiPlanError{category}
}

// aiPlanFailureMessage is the Thai failure message for a planning check (no internals).
func aiPlanFailureMessage(e error) (string, bool) {
	var plan *aiPlanError
	if !errors.As(e, &plan) {
		return "", false
	}
	switch plan.category {
	case "invalid_provider_response", "invalid_response_shape":
		return "AI ส่งแผนตัดต่อกลับมาในรูปแบบที่ใช้ไม่ได้ กดลองใหม่ได้เลย", true
	case "unknown_or_duplicate_evidence", "invalid_evidence", "overlapping_selection":
		return "AI เลือกช่วงคลิปที่ซ้ำ ทับกัน หรือไม่มีอยู่จริง กดลองใหม่ได้เลย", true
	case "invalid_draft_bounds_duration_or_metadata":
		return "แผนตัดต่อยาวเกินเป้าหมายหรือข้อมูลช่วงคลิปไม่ครบ กดลองใหม่ หรือลดความยาวเป้าหมาย", true
	case "invalid_composition":
		return "ใส่ข้อความหรือเสียงประกอบในฉบับร่างไม่ผ่านการตรวจสอบ กดลองใหม่ได้เลย", true
	}
	return "แผนตัดต่อไม่ผ่านการตรวจสอบ (" + plan.category + ") กดลองใหม่ได้เลย", true
}
