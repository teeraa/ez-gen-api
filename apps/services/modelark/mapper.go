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
}

// KnownModels is the authoritative in-process model registry.
// When a database model_configs table is added, this becomes the fallback.
var KnownModels = map[string]ModelConfig{
	"violet-motion-2": {
		InternalID:       "violet-motion-2",
		ProviderModelID:  "dreamina-seedance-2-0-fast-260128",
		DisplayName:      "Violet Motion 2.0",
		MaxResolution:    "720p",
		SupportedRatios:  []string{"16:9", "9:16", "1:1", "4:3", "3:4"},
		MinDuration:      4,
		MaxDuration:      15,
		SupportsAudio:    false,
		SupportsImgVideo: true,
	},
	"violet-cinema": {
		InternalID:       "violet-cinema",
		ProviderModelID:  "dreamina-seedance-2-0-260128",
		DisplayName:      "Violet Cinema",
		MaxResolution:    "4k",
		SupportedRatios:  []string{"16:9", "9:16", "1:1", "4:3", "21:9"},
		MinDuration:      4,
		MaxDuration:      15,
		SupportsAudio:    false,
		SupportsImgVideo: true,
	},
	"violet-fast": {
		InternalID:       "violet-fast",
		ProviderModelID:  "dreamina-seedance-2-0-fast-260128",
		DisplayName:      "Violet Fast",
		MaxResolution:    "720p",
		SupportedRatios:  []string{"16:9", "9:16", "1:1"},
		MinDuration:      4,
		MaxDuration:      15,
		SupportsAudio:    false,
		SupportsImgVideo: true,
	},
	"violet-realism": {
		InternalID:       "violet-realism",
		ProviderModelID:  "dreamina-seedance-2-5-260628",
		DisplayName:      "Violet Realism",
		MaxResolution:    "1080p",
		SupportedRatios:  []string{"16:9", "9:16", "1:1", "4:3", "21:9"},
		MinDuration:      4,
		MaxDuration:      30,
		SupportsAudio:    true,
		SupportsImgVideo: true,
	},
}

// QualityToResolution maps the application quality string to a provider resolution string.
func QualityToResolution(quality string) string {
	if quality == "high" {
		return "1080p"
	}
	return "720p"
}
