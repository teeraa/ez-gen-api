package dto_open_api

import "time"

type VoicePresetDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

// VoiceCapabilitiesDTO lets the Voice menu render its voice library and limits from the server.
type VoiceCapabilitiesDTO struct {
	Available bool             `json:"available"`
	Model     string           `json:"model"`
	MaxChars  int              `json:"maxChars"`
	Voices    []VoicePresetDTO `json:"voices"`
	Emotions  []string         `json:"emotions"`
	// Profiles ("my voices"): reference clips need ffmpeg on the server.
	MaxProfiles         int     `json:"maxProfiles"`
	ReferenceAvailable  bool    `json:"referenceAvailable"`
	ReferenceMinSeconds float64 `json:"referenceMinSeconds"`
	ReferenceMaxSeconds float64 `json:"referenceMaxSeconds"`
	ReferenceMaxBytes   int     `json:"referenceMaxBytes"`
}

// VoiceProfileDTO is one of the user's own voices. SampleURL (reference kind only) plays the stored clip.
type VoiceProfileDTO struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"`
	Description  string     `json:"description"`
	AudioSeconds float64    `json:"audioSeconds"`
	SampleURL    string     `json:"sampleUrl"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type VoiceSentenceDTO struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// VoiceGenerationDTO is one saved voice-over; the URLs are signed and expire at ExpiresAt.
type VoiceGenerationDTO struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	VoiceID   string `json:"voiceId"`
	VoiceName string `json:"voiceName"`
	// ProfileID is set when the voice was one of the user's own voices.
	ProfileID   *string            `json:"profileId"`
	Emotion     string             `json:"emotion"`
	Speed       float64            `json:"speed"`
	Pitch       int                `json:"pitch"`
	Duration    float64            `json:"duration"`
	SizeBytes   int64              `json:"sizeBytes"`
	AudioURL    string             `json:"audioUrl"`
	DownloadURL string             `json:"downloadUrl"`
	ExpiresAt   *time.Time         `json:"expiresAt"`
	Sentences   []VoiceSentenceDTO `json:"sentences"`
	CreatedAt   time.Time          `json:"createdAt"`
}

type VoiceGenerationListDTO struct {
	Items []VoiceGenerationDTO `json:"items"`
	Total int64                `json:"total"`
	Page  int                  `json:"page"`
	Limit int                  `json:"limit"`
}

// CharacterVoiceDTO is the voice bound to a character asset or film element.
type CharacterVoiceDTO struct {
	VoiceID    string  `json:"voiceId"`
	VoiceName  string  `json:"voiceName"`
	Kind       string  `json:"kind"`
	Emotion    string  `json:"emotion"`
	Speed      float64 `json:"speed"`
	Pitch      int     `json:"pitch"`
	TemplateID *string `json:"templateId"`
}
