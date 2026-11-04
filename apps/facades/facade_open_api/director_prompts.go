package facade_open_api

import (
	"fmt"
	"strings"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
)

// Seedream and Seedance follow English cinematography terms more reliably than Thai labels,
// so the user-facing Thai camera labels are translated when the provider prompt is built.
var directorCameraTerms = map[string]string{
	"มุมกว้าง":    "wide establishing shot",
	"มุมกลาง":     "medium shot",
	"โคลสอัพ":     "close-up shot",
	"มุมสูง":      "high-angle shot",
	"มุมต่ำ":      "low-angle shot",
	"มุมข้ามไหล่": "over-the-shoulder shot",
}

var directorCameraMotion = map[string]string{
	"มุมกว้าง":    "slow cinematic push-in, subtle parallax across the environment",
	"มุมกลาง":     "steady tracking shot following the action",
	"โคลสอัพ":     "gentle handheld micro-movement, shallow depth of field, focus on expressions",
	"มุมสูง":      "slow descending crane move",
	"มุมต่ำ":      "slow upward tilt emphasizing scale and power",
	"มุมข้ามไหล่": "slow arc around the foreground shoulder",
}

var directorStyleTerms = map[string]string{
	"cinematic":  "cinematic film still, anamorphic lens, dramatic motivated lighting, rich color grading, photorealistic detail",
	"anime":      "high-quality anime key visual, clean line art, cel shading, vibrant colors",
	"3d":         "stylized 3D animated feature film still, soft global illumination, expressive characters",
	"watercolor": "watercolor illustration, soft washes, visible paper texture, delicate edges",
	"clay":       "claymation stop-motion still, handcrafted clay figures, miniature practical set",
	"realistic":  "photorealistic, natural lighting, 35mm photography, true-to-life textures",
}

const directorPromptLimit = 2400

// Matches the Director cast limit and stays within Seedream's multi-reference input.
const directorMaxImageReferences = 4

// directorPlannedPrompt is the planner's English provider prompt for a shot. It is used only
// while the shot text still matches what the planner wrote; a user edit takes precedence.
type directorPlannedPrompt struct {
	Prompt string `json:"prompt"`
	Hash   string `json:"hash"`
}

func directorShotHash(description, camera string) string {
	return storyHash(struct{ Description, Camera string }{description, camera})
}

func directorShotAction(d *directorDocument, sh *dto.DirectorShot) string {
	if planned, ok := d.Prompts[sh.ID]; ok && planned.Prompt != "" && planned.Hash == directorShotHash(sh.Description, sh.Camera) {
		return planned.Prompt + "\nScene description (Thai): " + sh.Description
	}
	return sh.Description
}

func directorSceneSetting(d *directorDocument, sc *dto.DirectorScene) string {
	if sc == nil {
		return ""
	}
	if s := strings.TrimSpace(d.Settings[sc.ID]); s != "" {
		return s
	}
	return sc.Title + ": " + sc.Summary
}

func directorShotCharacters(chars []directorCharacter, ids []string) []directorCharacter {
	out := []directorCharacter{}
	for _, ch := range chars {
		if directorContains(ids, ch.ID) {
			out = append(out, ch)
		}
	}
	return out
}

func directorStyle(p *dto.DirectorProject) string {
	if s, ok := directorStyleTerms[p.Style]; ok {
		return s
	}
	return p.Style
}

// directorImagePrompt orders content by how strongly it should constrain the frame: the shot
// action first, then identity (bound to reference image order), then setting and world style.
func directorImagePrompt(d *directorDocument, p *dto.DirectorProject, sh *dto.DirectorShot, cast []directorCharacter, referenced int) string {
	sc := directorFindScene(p, sh.SceneID)
	var b strings.Builder
	fmt.Fprintf(&b, "%s.\n%s\n", directorCameraTerm(sh.Camera), directorShotAction(d, sh))
	if len(cast) > 0 {
		b.WriteString("Characters in this frame (all must appear, each exactly once):\n")
		for i, ch := range cast {
			ref := ""
			if i < referenced {
				ref = fmt.Sprintf(" — identical to reference Image %d (same face, hairstyle, body and outfit)", i+1)
			}
			fmt.Fprintf(&b, "- %s: %s%s\n", directorCharacterName(ch, i), directorTruncate(ch.Description, 300), ref)
		}
	} else {
		b.WriteString("Do not add named main characters that are not described.\n")
	}
	if setting := directorSceneSetting(d, sc); setting != "" {
		fmt.Fprintf(&b, "Location and lighting (keep consistent for every shot in this scene): %s\n", directorTruncate(setting, 500))
	}
	if bible := strings.TrimSpace(d.Bible); bible != "" {
		fmt.Fprintf(&b, "World and production design: %s\n", directorTruncate(bible, 500))
	}
	mood := strings.Join(p.Idea.Moods, ", ")
	if sc != nil && sc.Mood != nil {
		mood = *sc.Mood
	}
	fmt.Fprintf(&b, "Visual style: %s. Genre: %s. Mood: %s. Aspect ratio %s.\n", directorStyle(p), p.Idea.Genre, mood, p.AspectRatio)
	b.WriteString("Keep the same art style, color palette and character designs across the storyboard. No text, captions, logos or watermarks. Anatomically correct hands and faces.")
	return directorTruncate(b.String(), directorPromptLimit)
}

func directorVideoPrompt(d *directorDocument, p *dto.DirectorProject, sh *dto.DirectorShot, cast []directorCharacter, lastFrame bool) string {
	sc := directorFindScene(p, sh.SceneID)
	var b strings.Builder
	fmt.Fprintf(&b, "Animate this first frame as a %s. Camera movement: %s.\n", directorCameraTerm(sh.Camera), directorCameraMotionTerm(sh.Camera))
	if lastFrame {
		b.WriteString("End exactly on the supplied last frame (the next storyboard shot), moving naturally from the first composition to the last so consecutive clips cut together seamlessly.\n")
	}
	fmt.Fprintf(&b, "Action: %s\n", directorShotAction(d, sh))
	if len(cast) > 0 {
		names := []string{}
		for i, ch := range cast {
			names = append(names, directorCharacterName(ch, i))
		}
		fmt.Fprintf(&b, "Characters: %s. Keep every character's face, hairstyle and outfit exactly as in the first frame; do not add or remove characters.\n", strings.Join(names, ", "))
	}
	if setting := directorSceneSetting(d, sc); setting != "" {
		fmt.Fprintf(&b, "Setting: %s\n", directorTruncate(setting, 400))
	}
	fmt.Fprintf(&b, "Style: %s. Keep the background, lighting and color grade consistent with the first frame. Smooth natural motion, single continuous shot, no scene cuts, no text overlays.", directorStyle(p))
	return directorTruncate(b.String(), 1800)
}

func directorCameraTerm(camera string) string {
	if s, ok := directorCameraTerms[camera]; ok {
		return s
	}
	return camera
}

func directorCameraMotionTerm(camera string) string {
	if s, ok := directorCameraMotion[camera]; ok {
		return s
	}
	return "subtle cinematic camera movement"
}

func directorCharacterName(ch directorCharacter, i int) string {
	if n := strings.TrimSpace(ch.Name); n != "" {
		return n
	}
	return fmt.Sprintf("Character %d", i+1)
}

func directorCoverPrompt(p *dto.DirectorProject, cast []directorCharacter) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Key art poster for a %s story: %s\n", p.Idea.Genre, p.Idea.Prompt)
	for i, ch := range cast {
		fmt.Fprintf(&b, "- %s: %s — identical to reference Image %d\n", directorCharacterName(ch, i), directorTruncate(ch.Description, 300), i+1)
	}
	fmt.Fprintf(&b, "Visual style: %s. Mood: %s. Aspect ratio %s. Strong focal composition, no text, titles, logos or watermarks.", directorStyle(p), strings.Join(p.Idea.Moods, ", "), p.AspectRatio)
	return directorTruncate(b.String(), directorPromptLimit)
}
