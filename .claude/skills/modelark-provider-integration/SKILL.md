# Skill: modelark-provider-integration

Assemble the complete `apps/services/modelark/` provider package — from ContentItem
composition for all generation modes (text, image-to-video, Creative DNA references)
through draft mode, audio, and error mapping.

## Relationship to `integrate-modelark`

The existing `.claude/skills/integrate-modelark/SKILL.md` covers SDK setup, the
`Client` interface, basic ContentItem structure, status mapper, and poller skeleton.

**This skill extends that foundation.** Read `integrate-modelark` first, then use
this skill for the additional complexity:

| Covered by `integrate-modelark` | Covered HERE |
|---|---|
| SDK installation and client init | ContentItem array construction for all modes |
| Application `Client` interface | Character and style DNA reference injection |
| Basic `CreateTaskRequest` mapping | Draft mode two-pass workflow |
| Provider status → app status mapper | Audio generation handling |
| Simple poller skeleton | Per-model reference limits enforcement |
| Media download on success | Provider error code classification |

---

## When to Invoke

- When implementing `apps/services/modelark/requests.go` beyond the basic text-to-video case
- When adding DNA reference images to a generation request
- When implementing draft mode (Seedance 2.5)
- When adding audio generation support
- When the error mapper needs new provider error codes

---

## ContentItem Array Construction

The BytePlus API requires content items in a **specific order**: text first, images second.
Violating this order causes `InvalidParameter.TaskTypeConstraint`.

### Rules

```
content[0]      = text item (REQUIRED, always first)
content[1..N]   = image_url items (ordered by semantic role)
content[N+1..M] = video_url items (if any)
```

Role assignment by generation mode:

| Mode | Image Role | Max images per model |
|---|---|---|
| `text` | No images | — |
| `image_video` | `first_frame` (primary input image) | 1 |
| `image_image` | `first_frame` + `last_frame` | 2 |
| `dna_reference` | `reference_image` (character/style ref) | Model-specific max |

### Builder Pattern

`apps/services/modelark/content_builder.go`:

```go
package modelark

// ContentBuilder assembles a content array for a generation request.
// Enforces ordering and per-model limits.
type ContentBuilder struct {
    items    []ContentItem
    model    ModelCapability
    imgCount int
    vidCount int
}

func NewContentBuilder(model ModelCapability) *ContentBuilder {
    return &ContentBuilder{model: model}
}

// SetPrompt sets the text item. Must be called before any image method.
// prompt must not be empty.
func (b *ContentBuilder) SetPrompt(prompt string) *ContentBuilder {
    b.items = append([]ContentItem{{Type: "text", Text: prompt}}, b.items[1:]...)
    return b
}

// SetFirstFrame adds the primary input image for image-to-video mode.
// Returns error if model does not support SupportsImageVideo.
func (b *ContentBuilder) SetFirstFrame(storageURL string) error {
    if !b.model.SupportsImageVideo {
        return errors.New("model does not support first_frame input")
    }
    b.items = append(b.items, ContentItem{
        Type:     "image_url",
        ImageURL: &URLRef{URL: storageURL},
        Role:     "first_frame",
    })
    b.imgCount++
    return nil
}

// SetLastFrame adds the last-frame anchor for image-to-image mode.
// Must be called after SetFirstFrame.
func (b *ContentBuilder) SetLastFrame(storageURL string) error {
    hasFirst := false
    for _, item := range b.items {
        if item.Role == "first_frame" { hasFirst = true; break }
    }
    if !hasFirst {
        return errors.New("last_frame requires first_frame to be set first")
    }
    b.items = append(b.items, ContentItem{
        Type:     "image_url",
        ImageURL: &URLRef{URL: storageURL},
        Role:     "last_frame",
    })
    b.imgCount++
    return nil
}

// AddReferenceImage adds a DNA reference image (character or style).
// Returns error if model's MaxReferenceImages limit would be exceeded.
func (b *ContentBuilder) AddReferenceImage(storageURL string) error {
    if b.imgCount >= b.model.MaxReferenceImages {
        return fmt.Errorf("model allows at most %d reference images", b.model.MaxReferenceImages)
    }
    b.items = append(b.items, ContentItem{
        Type:     "image_url",
        ImageURL: &URLRef{URL: storageURL},
        Role:     "reference_image",
    })
    b.imgCount++
    return nil
}

// Build returns the final content array.
// Returns error if no text item is present.
func (b *ContentBuilder) Build() ([]ContentItem, error) {
    if len(b.items) == 0 || b.items[0].Type != "text" || b.items[0].Text == "" {
        return nil, errors.New("content array must begin with a non-empty text item")
    }
    return b.items, nil
}
```

---

## Draft Mode (Seedance 2.5 Only)

Draft mode generates a 480p preview at reduced cost before committing to full resolution.

### Two-Pass Workflow

**Pass 1 — Draft generation:**
```go
req := CreateTaskRequest{
    ProviderModelID: cap.ProviderModelID,
    Content:         content,
    Resolution:      "480p",
    Draft:           true,
    // ... other params
}
draftTaskID, err := client.CreateTask(ctx, req)
```

**Pass 2 — Finalize (reference draft task ID):**
```go
finalReq := CreateTaskRequest{
    ProviderModelID: cap.ProviderModelID,
    Content: []ContentItem{
        {Type: "text", Text: originalPrompt},
        {Type: "draft_task", DraftTask: &DraftTaskRef{ID: draftTaskID}},
    },
    Resolution: "1080p",
    Draft:      false,
}
finalTaskID, err := client.CreateTask(ctx, finalReq)
```

Application state:
- Job status `queued → submitted` on Pass 1 task creation
- Job status remains `processing` during Pass 2
- Job status `succeeded` only when Pass 2 completes

Store `draft_task_id` in a nullable column on `video_generation_jobs` to enable
Pass 2 submission. Add this column in a migration if not present.

---

## Audio Generation

Supported only on `violet-realism` (Seedance 2.5). Check `model.SupportsAudio` before setting.

```go
req.GenerateAudio = job.GenerateAudio && cap.SupportsAudio
```

If the user requested audio but the model does not support it:
- Return `VALIDATION_ERROR` with field `generateAudio` at the handler layer.
- Do not silently ignore the flag.

---

## Provider Error Classification

`apps/services/modelark/mapper.go` — classify provider errors into application types:

```go
type ProviderErrorClass int
const (
    ErrClassContent    ProviderErrorClass = iota // content policy — do not retry
    ErrClassInvalidParam                          // bad request — fix request
    ErrClassTransient                             // network/quota — retry with backoff
    ErrClassUnknown                               // treat as transient initially
)

func ClassifyProviderError(code string) ProviderErrorClass {
    switch code {
    case "OutputVideoSensitiveContentDetected":
        return ErrClassContent
    case "InvalidParameter.TaskTypeConstraint",
        "InvalidParameter.ContentOrderViolation": // [VERIFY this code exists]
        return ErrClassInvalidParam
    default:
        return ErrClassUnknown
    }
}
```

Rules for each class:
- `ErrClassContent` → job status `failed`, `failure_code: CONTENT_POLICY`, do NOT retry
- `ErrClassInvalidParam` → job status `failed`, `failure_code: INVALID_GENERATION_REQUEST`, do NOT retry
- `ErrClassTransient` → retry up to 3 times with exponential backoff; fail after 3
- `ErrClassUnknown` → treat as transient for first 2 retries, then fail

---

## Acceptance Criteria

- [ ] `ContentBuilder.Build()` returns error when text item is missing
- [ ] `ContentBuilder.Build()` returns error when `last_frame` is set without `first_frame`
- [ ] `ContentBuilder.AddReferenceImage` respects per-model limits
- [ ] Draft mode passes the draft task ID correctly to Pass 2
- [ ] `ClassifyProviderError` returns correct class for all known error codes
- [ ] No BytePlus SDK types exported outside this package
- [ ] `go test ./apps/services/modelark/...` passes without `ARK_API_KEY`

---

## Related Skills

- `integrate-modelark` — SDK setup and basic client interface (read first)
- `model-capability-registry` — provides `ModelCapability` used by ContentBuilder
- `creative-dna-prompt-compiler` — calls `AddReferenceImage` for DNA references
- `modelark-integration-testing` — tests ContentBuilder and error classifier
