package facade_open_api

import (
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
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

// Whole coarse segments are used only when fully contained in a cut. Partial
// cuts require word timestamps; transcript context is never treated as alignment.
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
			if speech.Start >= cut.Start-1e-6 && speech.End <= cut.End+1e-6 && utf8.RuneCountInString(speech.Text) <= 144 {
				result = append(result, speech)
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
	runes := []rune(text)
	if len(runes) > 144 {
		return ""
	}
	lines := []string{}
	for len(runes) > 24 {
		split := 24
		for split < len(runes) && unicode.Is(unicode.Mn, runes[split]) {
			split++
		}
		lines = append(lines, string(runes[:split]))
		runes = runes[split:]
	}
	if len(runes) > 0 {
		lines = append(lines, string(runes))
	}
	return strings.Join(lines, "\n")
}

func composeAIDraft(d *models.EditorAIDraft, in models.EditorAIInput, job string, transcripts []models.EditorAITranscript) {
	d.TextLayers = []models.VideoEditorTextLayer{}
	d.AudioClips = []models.VideoEditorAudioClip{}
	d.CompositionNotes = []string{}
	byAsset := map[string]models.EditorAITranscript{}
	for _, transcript := range transcripts {
		byAsset[transcript.AssetID] = transcript
	}
	room := max(0, veMaxTextLayers-in.ManualTextCount)
	add := func(text string, start, end, y float64, font int, key string) {
		text = aiCaptionLines(text)
		if text == "" || len(d.TextLayers) >= room || end-start < .001 {
			return
		}
		d.TextLayers = append(d.TextLayers, models.VideoEditorTextLayer{OriginJobID: job, ID: aiCompositionID(job, "text", key), Text: text, Start: start, End: end, X: .5, Y: y, FontSize: font, Color: "#FFFFFF", Align: "center"})
	}
	at := 0.0
	captionCount := 0
	titleCount := 0
	for _, cut := range d.Segments {
		length := cut.End - cut.Start
		if title := aiCompositionText(cut.Title); title != "" && len(d.TextLayers) < room {
			before := len(d.TextLayers)
			add(title, at, at+math.Min(length, 3), .15, 64, cut.ID+":title")
			if len(d.TextLayers) > before {
				titleCount++
			}
		}
		for _, caption := range aiCaptionWindows(byAsset[cut.AssetID], cut) {
			before := len(d.TextLayers)
			start := at + math.Max(0, caption.Start-cut.Start)
			end := at + math.Min(length, caption.End-cut.Start)
			add(caption.Text, start, end, .85, 48, fmt.Sprintf("%s:%f:%f", cut.ID, caption.Start, caption.End))
			if len(d.TextLayers) > before {
				captionCount++
			}
		}
		at += length
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
