package modelark

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ez-gen/ez-gen-api/apps/models"
)

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

// providerModels holds the capabilities of each BytePlus model the app can call.
// Adding a model here is what makes it valid for ARK_DEFAULT_MODEL.
var providerModels = map[string]ModelConfig{
	"seedance-1-0-pro-250528": {
		MaxResolution:    "1080p",
		SupportedRatios:  []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
		MinDuration:      2,
		MaxDuration:      12,
		SupportsAudio:    false,
		SupportsImgVideo: true,
		SupportsRefImage: false,
	},
	"dreamina-seedance-2-0-fast-260128": {
		MaxResolution:    "720p",
		SupportedRatios:  []string{"16:9", "9:16", "1:1", "4:3", "3:4"},
		MinDuration:      4,
		MaxDuration:      15,
		SupportsAudio:    false,
		SupportsImgVideo: true,
		SupportsRefImage: true,
	},
	"dreamina-seedance-2-0-260128": {
		MaxResolution:    "4k",
		SupportedRatios:  []string{"16:9", "9:16", "1:1", "4:3", "21:9"},
		MinDuration:      4,
		MaxDuration:      15,
		SupportsAudio:    false,
		SupportsImgVideo: true,
		SupportsRefImage: true,
	},
	"dreamina-seedance-2-5-260628": {
		MaxResolution:    "1080p",
		SupportedRatios:  []string{"16:9", "9:16", "1:1", "4:3", "21:9"},
		MinDuration:      4,
		MaxDuration:      30,
		SupportsAudio:    true,
		SupportsImgVideo: true,
		SupportsRefImage: true,
	},
}

type internalModel struct {
	displayName     string
	providerModelID string
}

// internalModels maps the model IDs the frontend sends to their target BytePlus model.
var internalModels = map[string]internalModel{
	"violet-motion-2": {"Violet Motion 2.0", "dreamina-seedance-2-0-fast-260128"},
	"violet-cinema":   {"Violet Cinema", "dreamina-seedance-2-0-260128"},
	"violet-fast":     {"Violet Fast", "dreamina-seedance-2-0-fast-260128"},
	"violet-realism":  {"Violet Realism", "dreamina-seedance-2-5-260628"},
	"violet-classic":  {"Violet Classic", "seedance-1-0-pro-250528"},
}

// Registry resolves internal model IDs to provider models. It is built once at
// startup so the routing chosen by ARK_DEFAULT_MODEL cannot change at runtime.
type Registry struct {
	models map[string]ModelConfig
}

// NewRegistry routes every internal model to overrideProviderModel when it is set,
// which lets an account that has activated only one model serve all of them.
// An empty override keeps each internal model's own provider mapping.
func NewRegistry(overrideProviderModel string) (*Registry, error) {
	if overrideProviderModel != "" {
		if _, ok := providerModels[overrideProviderModel]; !ok {
			return nil, fmt.Errorf("unsupported provider model %q (supported: %s)",
				overrideProviderModel, strings.Join(SupportedProviderModels(), ", "))
		}
	}
	models := make(map[string]ModelConfig, len(internalModels))
	for id, im := range internalModels {
		providerID := im.providerModelID
		if overrideProviderModel != "" {
			providerID = overrideProviderModel
		}
		cfg := providerModels[providerID]
		cfg.InternalID = id
		cfg.DisplayName = im.displayName
		cfg.ProviderModelID = providerID
		cfg.SupportedRatios = append([]string(nil), cfg.SupportedRatios...)
		models[id] = cfg
	}
	return &Registry{models: models}, nil
}

func (r *Registry) Lookup(internalID string) (ModelConfig, bool) {
	cfg, ok := r.models[internalID]
	return cfg, ok
}

// Routes returns internal ID → provider model ID, for startup logging.
func (r *Registry) Routes() map[string]string {
	out := make(map[string]string, len(r.models))
	for id, cfg := range r.models {
		out[id] = cfg.ProviderModelID
	}
	return out
}

func SupportedProviderModels() []string {
	ids := make([]string, 0, len(providerModels))
	for id := range providerModels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// QualityToResolution maps the application quality string to a provider resolution string.
func QualityToResolution(quality string) string {
	if quality == "high" {
		return "1080p"
	}
	return "720p"
}
