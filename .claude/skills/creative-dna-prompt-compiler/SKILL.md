# Skill: creative-dna-prompt-compiler

Compile a BytePlus-ready generation request (text prompt + ContentItem array) from
VIOLET STUDIO's Creative DNA layers: Character DNA, Style DNA, scene-specific
instructions, and reference image assets.

## What Is Creative DNA?

Creative DNA is VIOLET STUDIO's system for maintaining consistent visual identity
across multiple AI generations for a project.

| Layer | Meaning | Stored In |
|---|---|---|
| **Character DNA** | Visual identity of a recurring character: appearance, reference photo key | `character_dna` table (future) or per-project config |
| **Style DNA** | Visual aesthetic: color grading, film grain, lighting, camera style | `style_dna` table (future) or per-project config |
| **Scene instructions** | Shot-specific direction: action, camera movement, mood | User-supplied per generation |
| **Reference assets** | Pre-uploaded images used as `first_frame` or `reference_image` | `uploads` table, `output_media_key` from previous jobs |

---

## When to Invoke

- When implementing the prompt compilation step inside `submitToProvider`
- When a user reports visual inconsistency between generations in the same project
- When adding Character DNA or Style DNA configuration to a project
- When scene continuity requires the last frame of a previous generation

---

## Precedence Rules

When multiple DNA layers contribute to the same aspect (e.g., both Style DNA and
scene instructions describe lighting), resolve conflicts as follows:

```
scene instructions  (highest — per-shot override)
      ↓
character DNA anchor text  (character-level constraint)
      ↓
style DNA prefix  (project-level aesthetic)
      ↓
user's raw prompt  (lowest — baseline intent)
```

The compiled prompt is: `[StyleDNAPrefix] [CharacterAnchor] [UserPrompt] [SceneInstructions]`

**Never** silently drop any layer's contribution. If a conflict cannot be resolved,
preserve both and let the model decide — do not truncate either.

---

## Character DNA Structure

```go
// apps/models/character_dna.go (proposed)
type CharacterDNA struct {
    ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
    ProjectID          uuid.UUID
    Name               string    // "Luna" — used in prompt anchor
    PromptAnchor       string    // "a woman with silver hair, violet eyes, wearing a black leather jacket"
    ReferenceImageKey  *string   // object storage key — used as reference_image in ContentItem
    CreatedAt          time.Time
    UpdatedAt          time.Time
}
```

## Style DNA Structure

```go
// apps/models/style_dna.go (proposed)
type StyleDNA struct {
    ID           uuid.UUID `gorm:"type:uuid;primaryKey"`
    ProjectID    uuid.UUID
    PromptPrefix string    // "cinematic 35mm film, desaturated teal-orange palette, soft bokeh"
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

---

## Compiler Interface

`apps/services/dna/compiler.go`:

```go
package dna

// CompileInput contains everything needed to build a generation request.
type CompileInput struct {
    UserPrompt       string
    SceneInstructions string // optional per-shot direction
    Characters       []CharacterDNA
    Style            *StyleDNA
    FirstFrameKey    *string // object storage key for first_frame (image-to-video)
    LastFrameKey     *string // object storage key for last_frame (image-to-image)
    ModelCap         modelark.ModelCapability
}

// CompileOutput is ready to pass to ContentBuilder.
type CompileOutput struct {
    CompiledPrompt  string
    ContentItems    []modelark.ContentItem // text first, then images
    TruncatedLayers []string // names of layers that were truncated (warn, do not fail)
}

// Compile assembles the prompt and ContentItem array from DNA layers.
func Compile(input CompileInput) (CompileOutput, error) {
    if input.UserPrompt == "" {
        return CompileOutput{}, errors.New("user prompt is required")
    }
    // 1. Build text prompt
    compiled := buildPromptText(input)

    // 2. Enforce total prompt length limit (2000 chars per model default)
    maxLen := input.ModelCap.MaxPromptLength
    if maxLen == 0 { maxLen = 2000 }
    var truncated []string
    if len(compiled) > maxLen {
        compiled = compiled[:maxLen]
        truncated = append(truncated, "compiled_prompt")
    }

    // 3. Build ContentItem array
    builder := modelark.NewContentBuilder(input.ModelCap)
    builder.SetPrompt(compiled)

    if input.FirstFrameKey != nil {
        signedURL := storage.SignedURL(*input.FirstFrameKey)
        if err := builder.SetFirstFrame(signedURL); err != nil {
            return CompileOutput{}, fmt.Errorf("first_frame: %w", err)
        }
    }
    if input.LastFrameKey != nil {
        signedURL := storage.SignedURL(*input.LastFrameKey)
        if err := builder.SetLastFrame(signedURL); err != nil {
            return CompileOutput{}, fmt.Errorf("last_frame: %w", err)
        }
    }

    // 4. Add character reference images
    for i, char := range input.Characters {
        if char.ReferenceImageKey == nil { continue }
        signedURL := storage.SignedURL(*char.ReferenceImageKey)
        if err := builder.AddReferenceImage(signedURL); err != nil {
            // Model limit reached — record truncation, stop adding refs
            truncated = append(truncated, fmt.Sprintf("character_ref_%d", i))
            break
        }
    }

    items, err := builder.Build()
    if err != nil { return CompileOutput{}, err }

    return CompileOutput{
        CompiledPrompt: compiled,
        ContentItems:   items,
        TruncatedLayers: truncated,
    }, nil
}

func buildPromptText(input CompileInput) string {
    parts := []string{}
    if input.Style != nil && input.Style.PromptPrefix != "" {
        parts = append(parts, input.Style.PromptPrefix)
    }
    for _, char := range input.Characters {
        if char.PromptAnchor != "" {
            parts = append(parts, char.PromptAnchor)
        }
    }
    parts = append(parts, input.UserPrompt)
    if input.SceneInstructions != "" {
        parts = append(parts, input.SceneInstructions)
    }
    return strings.Join(parts, ". ")
}
```

---

## Scene Continuity

To create visual continuity between two generations:

1. Retrieve the completed previous job: `repo.FindByID(ctx, prevJobID, userID)`
2. Get the last-frame object storage key: `prevJob.OutputThumbKey`
   (or generate a last-frame by setting `return_last_frame: true` on the previous generation)
3. Pass `FirstFrameKey = &prevJob.OutputThumbKey` to `CompileInput`

This creates a `first_frame` ContentItem using the last frame of the prior generation,
maintaining visual continuity without any special provider API.

---

## Visual Consistency Disclaimer

No prompt technique guarantees pixel-perfect visual consistency between generations.
The reference image (`first_frame`, `reference_image`) guides but does not enforce
exact appearance. Do not promise exact character consistency to users.

---

## Prompt Logging

Log the compiled prompt at DEBUG level only, truncated to 150 characters:

```go
log.Debug("compiled prompt", "jobID", jobID, "prompt", truncate(compiled, 150))
```

Never log full prompts at INFO or higher — prompts may contain private user content.

---

## Acceptance Criteria

- [ ] `Compile` returns error on empty `UserPrompt`
- [ ] Compiled prompt order: Style prefix → Character anchor → User prompt → Scene instructions
- [ ] Reference images are rejected (not silently ignored) when per-model limit exceeded
- [ ] `first_frame` without `SupportsImageVideo: true` returns error
- [ ] `last_frame` without prior `first_frame` returns error
- [ ] Total prompt length stays within `MaxPromptLength`
- [ ] `TruncatedLayers` is non-empty when truncation occurs (not silently dropped)
- [ ] Unit tests cover: text-only, image-to-video, scene continuity, model limit exceeded

---

## Related Skills

- `model-capability-registry` — provides `ModelCapability.MaxReferenceImages`
- `modelark-provider-integration` — `ContentBuilder` is defined there
- `reference-assets-and-storage` — provides signed URLs for reference images
- `video-generation-lifecycle` — calls `Compile()` inside `submitToProvider`
