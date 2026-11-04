package facade_open_api

import (
	"fmt"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"math"
	"sort"
	"strings"
)

// Speech boundaries remain source context boundaries, not inferred sentence alignment.
func buildAIEvidence(in models.EditorAIInput, transcripts []models.EditorAITranscript, _ []models.EditorAIVisual) []models.EditorAIEvidence {
	byID := map[string]models.EditorAITranscript{}
	for _, t := range transcripts {
		byID[t.AssetID] = t
	}
	out := []models.EditorAIEvidence{}
	for _, source := range in.Sources {
		limit := 48 / len(in.Sources)
		if limit > 24 {
			limit = 24
		}
		sourceStart := len(out)
		seen := map[string]bool{}
		add := func(start, end float64, description string) {
			if len(out)-sourceStart >= limit || end <= start || end-start > in.TargetDuration+.001 {
				return
			}
			key := fmt.Sprintf("%.6f:%.6f", start, end)
			if seen[key] {
				return
			}
			seen[key] = true
			out = append(out, models.EditorAIEvidence{ID: uuid.NewString(), AssetID: source.ID, Start: start, End: end, Description: description})
		}
		transcript := byID[source.ID]
		if transcript.HasSpeech && len(transcript.Segments) > 0 {
			count := len(transcript.Segments)
			starts := limit / 2
			if starts < 3 {
				starts = 3
			}
			indexes := aiSampleIndexes(count, starts)
			for _, index := range indexes {
				segment := transcript.Segments[index]
				end := segment.End
				partial := end-segment.Start > in.TargetDuration
				if partial {
					end = segment.Start + in.TargetDuration
				}
				description := aiBoundedContext(aiSegmentContext(transcript, index, segment.Start, end))
				if partial {
					description = "Partial source context fallback; no verified word/sentence boundaries. " + description
				}
				add(segment.Start, end, description)
			}
			// Offer brief developments as well as a longer continuity fallback; neither is a mandated shot length.
			for _, units := range []int{2, count, 3} {
				runIndexes := indexes
				if units == 2 {
					runLimit := limit / 4
					if runLimit < 1 {
						runLimit = 1
					}
					runIndexes = aiSampleIndexes(count, runLimit)
				}
				for _, index := range runIndexes {
					segment := transcript.Segments[index]
					end, last := segment.End, index
					for next := index + 1; next < count && next < index+units; next++ {
						candidate := transcript.Segments[next]
						if candidate.End-segment.Start > in.TargetDuration+.001 || candidate.Start < end-.001 || in.RemoveSilence && candidate.Start-end > .35 {
							break
						}
						end, last = candidate.End, next
					}
					if last > index {
						add(segment.Start, end, fmt.Sprintf("Source speech contexts %d-%d; includes audio between them, not verified sentence boundaries. %s", index, last, aiBoundedContext(aiSegmentContext(transcript, index, segment.Start, segment.End))))
					}
					if units == count {
						break
					}
				}
			}
			continue
		}
		limit = 13
		duration := math.Min(in.TargetDuration, source.Duration)
		count := int(math.Ceil(source.Duration / duration))
		for _, index := range aiSampleIndexes(count, 12) {
			start := float64(index) * duration
			end := math.Min(start+duration, source.Duration)
			add(start, end, "Source frames sampled only; continuous source audio/video context, not a detected scene boundary")
		}
		if source.Duration > duration {
			add(source.Duration-duration, source.Duration, "Source frames sampled only; continuous tail context, not a detected scene boundary")
		}
	}
	return out
}
func aiSampleIndexes(count, limit int) []int {
	if count <= 0 {
		return nil
	}
	out := []int{}
	if limit == 1 {
		return []int{0}
	}
	if count <= limit {
		for i := 0; i < count; i++ {
			out = append(out, i)
		}
		return out
	}
	for i := 0; i < limit; i++ {
		out = append(out, i*(count-1)/(limit-1))
	}
	return out
}
func aiBoundedContext(text string) string {
	runes := []rune(text)
	if len(runes) > 112 {
		return string(runes[:64]) + "… " + string(runes[len(runes)-48:]) + " (see full transcript)"
	}
	return text
}
func aiEvidenceCapacity(evidence []models.EditorAIEvidence) float64 {
	byID := map[string][]models.EditorAIEvidence{}
	for _, e := range evidence {
		byID[e.AssetID] = append(byID[e.AssetID], e)
	}
	total := 0.0
	for _, rows := range byID {
		sort.Slice(rows, func(i, j int) bool { return rows[i].Start < rows[j].Start })
		start, end := rows[0].Start, rows[0].End
		for _, e := range rows[1:] {
			if e.Start <= end {
				end = math.Max(end, e.End)
			} else {
				total += end - start
				start, end = e.Start, e.End
			}
		}
		total += end - start
	}
	return total
}
func aiDurationReason(actual float64, proposed string, in models.EditorAIInput, evidence []models.EditorAIEvidence) string {
	if actual >= in.TargetDuration*.95-.001 {
		return ""
	}
	capacity := aiEvidenceCapacity(evidence)
	if len(evidence) > 0 && capacity < in.TargetDuration*.95-.001 {
		return fmt.Sprintf("ช่วงอ้างอิงที่เตรียมไว้มีเวลาไม่ซ้ำรวมประมาณ %.1f วินาที น้อยกว่าเป้า %.1f วินาที คุณยังเลือกใช้ฉบับนี้ได้", capacity, in.TargetDuration)
	}
	if strings.TrimSpace(proposed) != "" {
		return strings.TrimSpace(proposed)
	}
	return "AI เสนอฉบับสั้นกว่าเป้าเพื่อคงบริบทของช่วงที่เลือก ยังไม่ได้ยืนยันว่าต้นฉบับมีเนื้อหาไม่พอ คุณเลือกใช้ต่อหรือปรับฉบับร่างได้"
}
func aiConceptDuration(estimated float64, reason string, in models.EditorAIInput, evidence []models.EditorAIEvidence) (float64, string) {
	// An unexplained model default must not turn a full-length request into a shorter brief.
	if estimated < in.TargetDuration*.95 && strings.TrimSpace(reason) == "" {
		estimated = math.Min(aiEvidenceCapacity(evidence), in.TargetDuration)
	}
	if len(evidence) > 0 {
		estimated = math.Min(estimated, aiEvidenceCapacity(evidence))
	}
	return estimated, aiDurationReason(estimated, reason, in, evidence)
}
func aiSelectedOverlap(segments []models.EditorAIDraftSegment) bool {
	for i, a := range segments {
		for _, b := range segments[:i] {
			if a.AssetID == b.AssetID && a.Start < b.End-.001 && b.Start < a.End-.001 {
				return true
			}
		}
	}
	return false
}
func aiMergeAdjacent(segments []models.EditorAIDraftSegment) []models.EditorAIDraftSegment {
	out := []models.EditorAIDraftSegment{}
	for _, segment := range segments {
		if len(out) > 0 {
			previous := &out[len(out)-1]
			if previous.AssetID == segment.AssetID && math.Abs(previous.End-segment.Start) < .000001 && previous.Volume == segment.Volume && previous.Title == segment.Title && previous.Reason == segment.Reason {
				previous.End = segment.End
				continue
			}
		}
		out = append(out, segment)
	}
	return out
}
