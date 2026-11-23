package models

import "encoding/json"

// CharacterVoice is the voice bound to a character (library_assets.voice, film_elements.voice).
// VoiceName and Kind are snapshots taken when the voice was chosen, so lists need no lookups.
type CharacterVoice struct {
	VoiceID   string  `json:"voiceId"`
	VoiceName string  `json:"voiceName"`
	// Kind is "preset", "design" or "reference" (the user's own recorded voice).
	Kind    string  `json:"kind"`
	Emotion string  `json:"emotion"`
	Speed   float64 `json:"speed"`
	Pitch   int     `json:"pitch"`
	// TemplateID is the voice_generations row the settings were copied from, if any.
	TemplateID string `json:"templateId,omitempty"`
}

// EncodeCharacterVoice returns nil (SQL NULL) for no voice.
func EncodeCharacterVoice(v *CharacterVoice) []byte {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

// DecodeCharacterVoice returns nil for NULL or unreadable JSON.
func DecodeCharacterVoice(raw []byte) *CharacterVoice {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v CharacterVoice
	if json.Unmarshal(raw, &v) != nil || v.VoiceID == "" {
		return nil
	}
	return &v
}
