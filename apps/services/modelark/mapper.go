package modelark

import "github.com/ez-gen/ez-gen-api/apps/models"

// MapProviderStatus translates a provider status string to the application's JobStatus.
func MapProviderStatus(ps ProviderStatus) models.JobStatus {
	switch ps {
	case ProviderStatusQueued:
		return models.JobStatusSubmitted
	case ProviderStatusRunning:
		return models.JobStatusProcessing
	case ProviderStatusSucceeded:
		return models.JobStatusSucceeded
	case ProviderStatusFailed:
		return models.JobStatusFailed
	case ProviderStatusCancelled:
		return models.JobStatusCancelled
	default:
		return models.JobStatusProcessing
	}
}

// ModelConfig holds capability data for a single provider model.
type ModelConfig struct {
	InternalID       string
	ProviderModelID  string
	DisplayName      string
	MaxResolution    string
	SupportedRatios  []string
	MinDuration      int
	MaxDuration      int
	SupportsAudio    bool
	SupportsImgVideo bool
	SupportsRefImage bool
}

// SupportsOutput reports whether the model accepts the given aspect ratio and duration.
// Checking here turns a provider-side 4xx into a MODEL_NOT_SUPPORTED response.
func (m ModelConfig) SupportsOutput(ratio string, durationSeconds int) bool {
	if durationSeconds < m.MinDuration || durationSeconds > m.MaxDuration {
		return false
	}
	for _, r := range m.SupportedRatios {
		if r == ratio {
			return true
		}
	}
	return false
}

// seedanceOneProModelID is the only model activated on the ModelArk account, so every
// internal ID routes to it until the Seedance 2.x models are activated.
const seedanceOneProModelID = "seedance-1-0-pro-250528"

func seedanceOnePro(internalID, displayName string) ModelConfig {
	return ModelConfig{
		InternalID:       internalID,
		ProviderModelID:  seedanceOneProModelID,
		DisplayName:      displayName,
		MaxResolution:    "1080p",
		SupportedRatios:  []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
		MinDuration:      2,
		MaxDuration:      12,
		SupportsAudio:    false,
		SupportsImgVideo: true,
		SupportsRefImage: false,
	}
}

// KnownModels is the authoritative in-process model registry.
// When a database model_configs table is added, this becomes the fallback.
var KnownModels = map[string]ModelConfig{
	"violet-motion-2": seedanceOnePro("violet-motion-2", "Violet Motion 2.0"),
	"violet-cinema":   seedanceOnePro("violet-cinema", "Violet Cinema"),
	"violet-fast":     seedanceOnePro("violet-fast", "Violet Fast"),
	"violet-realism":  seedanceOnePro("violet-realism", "Violet Realism"),
	"violet-classic":  seedanceOnePro("violet-classic", "Violet Classic"),
}

// QualityToResolution maps the application quality string to a provider resolution string.
func QualityToResolution(quality string) string {
	if quality == "high" {
		return "1080p"
	}
	return "720p"
}
