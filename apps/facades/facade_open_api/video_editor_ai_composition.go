package facade_open_api

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/ez-gen/ez-gen-api/apps/services/thaiseg"
	"github.com/google/uuid"
)

func aiCompositionID(job, kind, key string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(job+":"+kind+":"+key)).String()
}

func aiCompositionText(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	text = strings.Map(func(r rune) rune {
		if veTextRejected(r) {
			return -1
		}
		return r
	}, text)
	return strings.TrimSpace(text)
}

// aiCaptionMaxRunes bounds one caption (two 24-rune lines, see aiCaptionLines).
const aiCaptionMaxRunes = 48

// aiCaptionMinSeconds drops captions too short to read after clamping to their clip.
const aiCaptionMinSeconds = .3

// aiCaptionSpan is the part of clip i shown on its own: after the transition from the
// previous clip and before the transition into the next one. Captions stay inside it so
// a caption never runs into another clip (where it would overlap that clip's captions).
func aiCaptionSpan(at, start, length float64, i int, cuts []models.EditorAIDraftSegment) (float64, float64) {
	next := 0.0
	if i+1 < len(cuts) {
		next = cuts[i+1].TimelineSegment().TransitionDuration
	}
	return aiOwnSpan(at, start, length, i > 0, next)
}

// aiOwnSpan: a clip placed at start (at = where the previous clip ends) is shared with
// the previous clip until at, and with the next one for its transition at the end.
func aiOwnSpan(at, start, length float64, hasPrevious bool, nextTransition float64) (float64, float64) {
	own, end := start, start+length-nextTransition
	if hasPrevious {
		own = math.Max(start, at)
	}
	return own, math.Max(own, end)
}

// aiCaptionMaxLines matches the editor's text-layer limit.
const aiCaptionMaxLines = 6

// aiCaptionLineWidth is the visible width of one caption line (two lines per caption).
const aiCaptionLineWidth = 24

func aiThaiNoBreakBefore(r rune) bool { return thaiseg.NoBreakBefore(r) }
func aiThaiLeadingVowel(r rune) bool  { return thaiseg.LeadingVowel(r) }

// aiCaptionFits reports whether text is one caption: at most max runes and two lines
// when wrapped between words.
func aiCaptionFits(text string, max int) bool {
	text = strings.TrimSpace(text)
	return utf8.RuneCountInString(text) <= max && len(thaiseg.Wrap(text, aiCaptionLineWidth)) <= 2
}

// aiSplitCaptionText splits text into captions that each fit two lines, breaking only
// between words (Thai words found with a dictionary, see thaiseg). A single word too long
// for a caption is cut at syllable-safe points.
func aiSplitCaptionText(text string, max int) []string {
	out, current := []string{}, ""
	flush := func() {
		if piece := strings.TrimSpace(current); piece != "" {
			out = append(out, piece)
		}
		current = ""
	}
	for _, token := range thaiseg.Segment(text) {
		if strings.TrimSpace(token) == "" {
			if current != "" {
				current += " "
			}
			continue
		}
		if aiCaptionFits(current+token, max) {
			current += token
			continue
		}
		flush()
		if aiCaptionFits(token, max) {
			current = token
			continue
		}
		lines := thaiseg.Wrap(token, aiCaptionLineWidth)
		for len(lines) > 0 {
			n := min(2, len(lines))
			piece := strings.Join(lines[:n], "")
			lines = lines[n:]
			if len(lines) == 0 {
				current = piece
				break
			}
			out = append(out, piece)
		}
	}
	flush()
	return out
}

// aiSplitCoarseSpeech turns one word-less speech segment (e.g. Typhoon contexts of up
// to 12 s) into short captions. Times are ESTIMATED by spreading the segment's duration
// over the pieces in proportion to their length; they are not word alignment.
func aiSplitCoarseSpeech(speech models.EditorAISpeech) []models.EditorAISpeech {
	text := strings.Join(strings.Fields(aiCompositionText(speech.Text)), " ")
	pieces := aiSplitCaptionText(text, aiCaptionMaxRunes)
	if len(pieces) <= 1 {
		if len(pieces) == 1 {
			speech.Text = pieces[0]
		}
		return []models.EditorAISpeech{speech}
	}
	weights, total := make([]float64, len(pieces)), 0.0
	for i, piece := range pieces {
		weights[i] = float64(utf8.RuneCountInString(strings.ReplaceAll(piece, " ", "")))
		total += weights[i]
	}
	out, at := make([]models.EditorAISpeech, 0, len(pieces)), speech.Start
	for i, piece := range pieces {
		end := speech.End
		if i < len(pieces)-1 && total > 0 {
			end = at + (speech.End-speech.Start)*weights[i]/total
		}
		out = append(out, models.EditorAISpeech{Start: at, End: end, Text: piece})
		at = end
	}
	return out
}

// Whole coarse segments are used only when fully contained in a cut; long ones are
// split into short captions with estimated timing inside that segment. Partial cuts
// require word timestamps; transcript context is never treated as alignment.
func aiCaptionWindows(transcript models.EditorAITranscript, cut models.EditorAIDraftSegment) []models.EditorAISpeech {
	result := []models.EditorAISpeech{}
	for _, speech := range transcript.Segments {
		if speech.End <= cut.Start || speech.Start >= cut.End {
			continue
		}
		uncertainWords := false
		for _, word := range speech.Words {
			if word.End <= word.Start {
				uncertainWords = true
				break
			}
		}
		if len(speech.Words) == 0 || uncertainWords {
			if speech.Start >= cut.Start-1e-6 && speech.End <= cut.End+1e-6 {
				result = append(result, aiSplitCoarseSpeech(speech)...)
			}
			continue
		}
		var group models.EditorAISpeech
		flush := func() {
			if group.Text != "" {
				result = append(result, group)
				group = models.EditorAISpeech{}
			}
		}
		for _, word := range speech.Words {
			if word.Start < cut.Start-1e-6 || word.End > cut.End+1e-6 || word.End <= word.Start {
				flush()
				continue
			}
			text := aiCompositionText(word.Text)
			if text == "" {
				continue
			}
			if group.Text != "" && (utf8.RuneCountInString(group.Text)+utf8.RuneCountInString(text) > 48 || word.End-group.Start > 3.5 || word.Start-group.End > .7) {
				flush()
			}
			if group.Text == "" {
				group.Start = word.Start
				group.Text = text
			} else {
				group.Text += " " + text
			}
			group.End = word.End
		}
		flush()
	}
	return result
}

func aiCaptionLines(text string) string {
	text = strings.Join(strings.Fields(aiCompositionText(text)), " ")
	if utf8.RuneCountInString(text) > 144 {
		return ""
	}
	lines := thaiseg.Wrap(text, aiCaptionLineWidth)
	// Text layers allow at most 6 lines (normalizeVETextLayers). Wrapping between words can
	// need more than 144/24 lines, and one over-long layer would fail the whole draft.
	if len(lines) > aiCaptionMaxLines {
		return ""
	}
	return strings.Join(lines, "\n")
}

func composeAIDraft(d *models.EditorAIDraft, in models.EditorAIInput, job string, transcripts []models.EditorAITranscript) {
	composeAIDraftWith(d, in, job, transcripts, nil)
}

// composeAIDraftWith also captions extra speech per cut ID: parts of word-less segments
// that a cut crosses, re-transcribed on their own (see partialCaptions).
func composeAIDraftWith(d *models.EditorAIDraft, in models.EditorAIInput, job string, transcripts []models.EditorAITranscript, extra map[string][]models.EditorAISpeech) {
	d.TextLayers = []models.VideoEditorTextLayer{}
	d.AudioClips = []models.VideoEditorAudioClip{}
	d.CompositionNotes = []string{}
	byAsset := map[string]models.EditorAITranscript{}
	for _, transcript := range transcripts {
		byAsset[transcript.AssetID] = transcript
	}
	room := max(0, veMaxTextLayers-in.ManualTextCount)
	var captionStyle, titleStyle *models.EditorAITextStyle
	if d.TextStyles != nil {
		captionStyle, titleStyle = d.TextStyles.Caption, d.TextStyles.Title
	}
	keys := map[string]int{}
	add := func(text string, start, end, y float64, font int, key string, style *models.EditorAITextStyle) {
		text = aiCaptionLines(text)
		if text == "" || len(d.TextLayers) >= room || end-start < .001 {
			return
		}
		// Accepted transcripts can repeat a timestamp pair; IDs must stay unique.
		if n := keys[key]; n > 0 {
			keys[key]++
			key = fmt.Sprintf("%s#%d", key, n)
		} else {
			keys[key] = 1
		}
		layer := models.VideoEditorTextLayer{OriginJobID: job, ID: aiCompositionID(job, "text", key), Text: text, Start: start, End: end, X: .5, Y: y, FontSize: font, Color: "#FFFFFF", Align: "center"}
		d.TextLayers = append(d.TextLayers, aiApplyTextStyle(layer, style))
	}
	at := 0.0
	captionCount := 0
	titleCount := 0
	for i, cut := range d.Segments {
		segment := cut.TimelineSegment()
		// A transition starts this segment that much earlier, overlapping the previous one.
		start := math.Max(0, at-segment.TransitionDuration)
		length := segment.TimelineLength()
		ownStart, ownEnd := aiCaptionSpan(at, start, length, i, d.Segments)
		speed := models.SegmentSpeed(segment.Speed)
		if title := aiCompositionText(cut.Title); title != "" && len(d.TextLayers) < room {
			before := len(d.TextLayers)
			add(title, start, start+math.Min(length, 3), .15, 64, cut.ID+":title", titleStyle)
			if len(d.TextLayers) > before {
				titleCount++
			}
		}
		captions := aiCaptionWindows(byAsset[cut.AssetID], cut)
		if len(extra[cut.ID]) > 0 {
			captions = append(captions, aiCaptionWindows(models.EditorAITranscript{Segments: extra[cut.ID]}, cut)...)
		}
		for _, caption := range captions {
			before := len(d.TextLayers)
			captionStart := math.Max(ownStart, start+math.Max(0, caption.Start-cut.Start)/speed)
			captionEnd := math.Min(ownEnd, start+math.Min(length, (caption.End-cut.Start)/speed))
			if captionEnd-captionStart < aiCaptionMinSeconds {
				continue
			}
			add(caption.Text, captionStart, captionEnd, .85, 48, fmt.Sprintf("%s:%f:%f", cut.ID, caption.Start, caption.End), captionStyle)
			if len(d.TextLayers) > before {
				captionCount++
			}
		}
		at = start + length
	}
	if captionCount == 0 {
		d.CompositionNotes = append(d.CompositionNotes, "ไม่มีคำถอดเสียงที่ระบุตำแหน่งตรงกับช่วงตัดอย่างปลอดภัย จึงไม่สร้างซับจากข้อความที่คาดเดา")
	}
	if len(d.TextLayers) >= room {
		d.CompositionNotes = append(d.CompositionNotes, "จำกัดจำนวนข้อความเพื่อเก็บชั้นที่คุณใส่ไว้ โดยไม่ลบข้อความของคุณ")
	}
	if titleCount > 0 {
		d.CompositionNotes = append(d.CompositionNotes, "เพิ่มหัวข้อจากแผนตัดต่อที่อ้างอิงคลิป และซับเฉพาะคำที่มีเวลาตรงกับต้นฉบับ")
	}
	if in.ManualAudio {
		d.CompositionNotes = append(d.CompositionNotes, "เก็บเสียงที่คุณวางไว้ จึงไม่เพิ่มเพลงซ้อน")
	} else if len(in.AudioSources) > 0 && at > 0 {
		source := in.AudioSources[0]
		d.AudioClips = append(d.AudioClips, models.VideoEditorAudioClip{OriginJobID: job, ID: aiCompositionID(job, "audio", source.ID), AssetID: source.ID, At: 0, Start: 0, End: math.Min(at, source.Duration), Volume: .12})
		d.CompositionNotes = append(d.CompositionNotes, "ใช้ไฟล์เสียงที่อัปโหลดไว้เป็นพื้นหลังระดับเบา 12% โดยไม่สร้างเสียงพูดหรือเพลงใหม่")
		if source.Duration < at {
			d.CompositionNotes = append(d.CompositionNotes, "ไฟล์เสียงพื้นหลังสั้นกว่าวิดีโอ ใช้ถึงท้ายไฟล์โดยไม่วนซ้ำ")
		}
	} else {
		d.CompositionNotes = append(d.CompositionNotes, "ไม่มีไฟล์เสียงพื้นหลังที่พร้อมใช้ เก็บเสียงต้นฉบับตามค่าที่เลือก โดยไม่สร้างเพลงหรือเสียงบรรยายใหม่")
	}
}

func validateAIComposition(d models.EditorAIDraft, in models.EditorAIInput, job string) error {
	if len(d.AudioClips) > veMaxAudioClips || len(d.TextLayers) > veMaxTextLayers {
		return editorai.ErrInvalid
	}
	if _, err := normalizeVETextLayers(d.TextLayers); err != nil {
		return editorai.ErrInvalid
	}
	for _, text := range d.TextLayers {
		if text.OriginJobID != job || text.End > d.Duration+.001 {
			return editorai.ErrInvalid
		}
	}
	sources := map[string]models.EditorAISource{}
	for _, source := range in.AudioSources {
		sources[source.ID] = source
	}
	for _, clip := range d.AudioClips {
		source, ok := sources[clip.AssetID]
		if !ok || !veID(clip.ID) || clip.OriginJobID != job || !aiFinite(clip.Start) || !aiFinite(clip.End) || !aiFinite(clip.At) || !aiFinite(clip.Volume) || clip.Start < 0 || clip.End <= clip.Start || clip.End > source.Duration+.001 || clip.At < 0 || clip.At+clip.End-clip.Start > d.Duration+.001 || clip.Volume < 0 || clip.Volume > 1 {
			return editorai.ErrInvalid
		}
	}
	return nil
}

func validateAIApplyTracks(project *models.VideoEditorProject, d models.EditorAIDraft, assets []models.VideoEditorAsset) error {
	audio := []models.VideoEditorAudioClip{}
	text := []models.VideoEditorTextLayer{}
	for _, clip := range veAudioClips(project) {
		if !veID(clip.OriginJobID) {
			audio = append(audio, clip)
		}
	}
	for _, layer := range veTextLayers(project) {
		if !veID(layer.OriginJobID) {
			text = append(text, layer)
		}
	}
	if len(audio) == 0 {
		audio = append(audio, d.AudioClips...)
	}
	text = append(text, d.TextLayers...)
	if err := validateVEAudioClips(audio, assets); err != nil {
		return err
	}
	_, err := normalizeVETextLayers(text)
	return err
}
