# Skill: model-capability-registry

Maintain the authoritative mapping between VIOLET STUDIO internal model identifiers
and BytePlus model IDs, with per-model capability constraints used for request
validation.

## When to Invoke

- Before implementing `POST /open/v1/jobs` request validation
- When adding a new model to the platform
- When a BytePlus model is deprecated or renamed
- When a model's parameters change (new resolution tier, duration range update)
- When validating that a user request is compatible with the selected model

## Inputs

- A model change request: add / update / deprecate a model
- Or: a capability question ("does violet-realism support image-to-video?")
- Requires: `docs/engineering/modelark-integration-research.md` to be current

## Outputs

- Updated capability source (Go constants file or DB seed migration)
- Updated `docs/engineering/modelark-integration-research.md` Section 6 if mapping changed
- No frontend changes — this is backend-only

---

## Representation Decision

The model registry can be implemented as either:

| Option | File | When to use |
|---|---|---|
| **Go constants** | `apps/services/modelark/models.go` | Phase 1 — simpler, no DB required |
| **DB table seed** | `migrations/NNN_seed_model_configs.sql` | Phase 2 — when runtime config needed |

**Current recommendation:** Start with Go constants. Migrate to DB when models need
to be updated without a deployment.

Both representations must express the same schema. The DB table is defined in
`docs/engineering/video-data-model-proposal.md` Section 2.5.

---

## Go Constants Format

`apps/services/modelark/models.go`:

```go
package modelark

// ModelCapability describes what a BytePlus model supports.
type ModelCapability struct {
    ProviderModelID    string
    DisplayName        string
    MaxResolution      string
    DurationRange      [2]int // [min, max] seconds
    SupportsAudio      bool
    SupportsDraftMode  bool
    SupportsImageVideo bool   // first_frame input
    SupportsImageImage bool   // first_frame + last_frame
    MaxReferenceImages int
    MaxReferenceVideos int
    SupportedRatios    []string
    SupportedDurations []int  // specific allowed values, if constrained
}

// ModelRegistry maps VIOLET STUDIO internal IDs to BytePlus capabilities.
// Source of truth: docs/engineering/modelark-integration-research.md Section 6.
// Last verified: [DATE]
var ModelRegistry = map[string]ModelCapability{
    "violet-motion-2": {
        ProviderModelID:    "dreamina-seedance-2-0-fast-260128",
        DisplayName:        "Violet Motion 2.0",
        MaxResolution:      "720p",
        DurationRange:      [2]int{4, 15},
        SupportsAudio:      false,
        SupportsDraftMode:  false,
        SupportsImageVideo: true,
        SupportsImageImage: false,
        MaxReferenceImages: 9,
        MaxReferenceVideos: 3,
        SupportedRatios:    []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
        SupportedDurations: []int{5, 10},
    },
    "violet-cinema": {
        ProviderModelID:    "dreamina-seedance-2-0-260128",
        DisplayName:        "Violet Cinema",
        MaxResolution:      "1080p",
        DurationRange:      [2]int{4, 15},
        SupportsAudio:      false,
        SupportsDraftMode:  false,
        SupportsImageVideo: true,
        SupportsImageImage: false,
        MaxReferenceImages: 9,
        MaxReferenceVideos: 3,
        SupportedRatios:    []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
        SupportedDurations: []int{5, 10},
    },
    "violet-fast": {
        // [VERIFY] Whether this maps to the same BytePlus model as violet-motion-2
        ProviderModelID:    "dreamina-seedance-2-0-fast-260128",
        DisplayName:        "Violet Fast",
        MaxResolution:      "720p",
        DurationRange:      [2]int{4, 15},
        SupportsAudio:      false,
        SupportsDraftMode:  false,
        SupportsImageVideo: true,
        SupportsImageImage: false,
        MaxReferenceImages: 9,
        MaxReferenceVideos: 3,
        SupportedRatios:    []string{"16:9", "9:16", "1:1"},
        SupportedDurations: []int{5, 10},
    },
    "violet-realism": {
        ProviderModelID:    "dreamina-seedance-2-5-260628",
        DisplayName:        "Violet Realism",
        MaxResolution:      "1080p",
        DurationRange:      [2]int{4, 30},
        SupportsAudio:      true,
        SupportsDraftMode:  true,
        SupportsImageVideo: true,
        SupportsImageImage: false,  // [VERIFY]
        MaxReferenceImages: 30,
        MaxReferenceVideos: 10,
        SupportedRatios:    []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"},
        SupportedDurations: []int{5, 10},
    },
}

// Lookup returns a model capability or false if the internal ID is not registered.
func Lookup(internalModelID string) (ModelCapability, bool) {
    cap, ok := ModelRegistry[internalModelID]
    return cap, ok
}

// ValidateRequest checks that a generation request is compatible with the model.
// Returns a list of validation error messages (empty = valid).
func ValidateRequest(modelID, mode, ratio string, duration int, audioRequested bool) []string {
    cap, ok := Lookup(modelID)
    if !ok {
        return []string{"unknown model: " + modelID}
    }
    var errs []string
    if mode == "image_video" && !cap.SupportsImageVideo {
        errs = append(errs, "model does not support image-to-video mode")
    }
    if audioRequested && !cap.SupportsAudio {
        errs = append(errs, "model does not support audio generation")
    }
    if duration < cap.DurationRange[0] || duration > cap.DurationRange[1] {
        errs = append(errs, fmt.Sprintf("duration must be between %d and %d seconds", cap.DurationRange[0], cap.DurationRange[1]))
    }
    ratioOK := false
    for _, r := range cap.SupportedRatios {
        if r == ratio { ratioOK = true; break }
    }
    if !ratioOK {
        errs = append(errs, "unsupported aspect ratio for this model: "+ratio)
    }
    return errs
}
```

---

## Workflow: Adding a New Model

1. Run `modelark-api-research` to confirm the new model's capabilities.
2. Add an entry to `ModelRegistry` with all fields populated.
3. Mark any unverified fields with `// [VERIFY]` comment.
4. Add a unit test in `apps/services/modelark/models_test.go`:
   ```go
   func TestModelRegistry_NewModel(t *testing.T) {
       cap, ok := Lookup("violet-new-model")
       require.True(t, ok)
       assert.Equal(t, "byteplus-model-id", cap.ProviderModelID)
   }
   ```
5. Update `docs/engineering/modelark-integration-research.md` Section 6.
6. Run `go test ./apps/services/modelark/...`.

## Workflow: Deprecating a Model

1. Set `is_active: false` in DB (if using DB representation) OR
   remove from `ModelRegistry` after confirming no in-flight jobs use it.
2. Before removing, check: `SELECT COUNT(*) FROM video_generation_jobs WHERE model_id = 'old-model' AND status IN ('queued','submitted','processing')`.
3. If count > 0, do not remove — wait for in-flight jobs to complete.
4. Add a comment documenting when and why the model was deprecated.

---

## Acceptance Criteria

- [ ] Every internal model ID has a confirmed BytePlus `ProviderModelID`
- [ ] `[VERIFY]` comments are resolved or escalated
- [ ] `ValidateRequest` rejects mode/duration/ratio combos unsupported by the model
- [ ] Unit tests cover: valid request, invalid mode, out-of-range duration, unsupported ratio
- [ ] `go test ./apps/services/modelark/...` passes

---

## Related Skills

- `modelark-api-research` — provides confirmed model IDs and capabilities
- `modelark-provider-integration` — uses `Lookup()` to get `ProviderModelID`
- `creative-dna-prompt-compiler` — uses `MaxReferenceImages` to limit DNA references
- `modelark-integration-testing` — tests the registry and `ValidateRequest`
