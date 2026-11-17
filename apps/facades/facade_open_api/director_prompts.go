package facade_open_api

import (
	"fmt"
	"sort"
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
	"cinematic":  "cinematic live-action film still, anamorphic lens, dramatic motivated lighting, rich teal-and-orange color grading, shallow depth of field, photorealistic detail",
	"anime":      "2D anime illustration, Japanese anime key visual, clean line art, flat cel shading, vibrant colors, not photorealistic, not 3D",
	"3d":         "stylized 3D animated feature film render, Pixar-like proportions, soft global illumination, smooth CG materials, not photorealistic",
	"watercolor": "hand-painted watercolor illustration, soft washes and bleeding edges, visible paper texture, not photorealistic",
	"clay":       "claymation stop-motion still, handcrafted plasticine figures with fingerprint texture, miniature practical set, not photorealistic",
	"realistic":  "photorealistic live-action photograph, natural lighting, 35mm lens, true-to-life skin and fabric textures",
}

const directorPromptLimit = 2400

// Matches the Director cast limit and stays within Seedream's multi-reference input.
const directorMaxImageReferences = 4

// Seedance 2.0 / Fast accept up to 9 reference images; two are the storyboard frames.
const directorMaxVideoReferences = 6

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

// directorObjectIntegrity and directorFaceIntegrity target the most common generation defects:
// handles/hilts duplicated at both ends, scabbards drawn as hilts, bent umbrellas, merged faces.
const directorObjectIntegrity = "Objects keep their real-world structure: a sword, umbrella or tool has exactly one handle or hilt at one end, never a handle at both ends; a scabbard is a separate sheath, never a second hilt; straight shafts and blades stay straight; no duplicated, fused, mirrored or extra parts."
const directorFaceIntegrity = "Each person has exactly one head and one face; faces never merge, duplicate, overlap or distort; keep clear space between heads."

const directorMaxProps = 6

func directorNormalizeProps(in []directorProp) []directorProp {
	out, seen := []directorProp{}, map[string]bool{}
	for _, p := range in {
		name, spec := directorTruncate(strings.TrimSpace(p.Name), 60), directorTruncate(strings.TrimSpace(p.Spec), 180)
		key := strings.ToLower(name)
		if name == "" || spec == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, directorProp{Name: name, Spec: spec})
		if len(out) == directorMaxProps {
			break
		}
	}
	return out
}

// directorPropLines restates the canonical structure of at most two props named in the shot,
// followed by the object and face integrity rules that apply to every shot.
func directorPropLines(d *directorDocument, action string) string {
	var b strings.Builder
	lower, n := strings.ToLower(action), 0
	for _, p := range d.Props {
		if n < 2 && strings.Contains(lower, strings.ToLower(p.Name)) {
			fmt.Fprintf(&b, "Prop %s (exact structure, identical in every shot): %s\n", p.Name, p.Spec)
			n++
		}
	}
	b.WriteString(directorObjectIntegrity + "\n" + directorFaceIntegrity + "\n")
	return b.String()
}

func directorSceneSetting(d *directorDocument, sc *dto.DirectorScene) string {
	if sc == nil {
		return ""
	}
	if s := strings.TrimSpace(sc.Setting); s != "" {
		return s
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

func directorStyleShort(p *dto.DirectorProject) string {
	style := directorStyle(p)
	if i := strings.Index(style, ","); i > 0 {
		return style[:i]
	}
	return style
}

func directorStyle(p *dto.DirectorProject) string {
	if s, ok := directorStyleTerms[p.Style]; ok {
		return s
	}
	return p.Style
}

// directorImagePrompt orders content by how strongly it should constrain the frame: the shot
// action first, then identity (bound to reference image order), then setting and world style.
// directorImagePrompt states the selected art style first and last: character references carry
// their own rendering (often photoreal), and models copy it unless told the style is fixed.
func directorImagePrompt(d *directorDocument, p *dto.DirectorProject, sh *dto.DirectorShot, cast []directorCharacter, referenced int) string {
	sc := directorFindScene(p, sh.SceneID)
	var b strings.Builder
	fmt.Fprintf(&b, "Art style (mandatory for the whole image, overrides the style of any reference image): %s.\n", directorStyle(p))
	// Location precedes the cast: references are usually shot on plain backgrounds and shot
	// descriptions often omit the place.
	setting := directorSceneSetting(d, sc)
	if setting != "" {
		fmt.Fprintf(&b, "Location (fully rendered, identical in every shot of this scene): %s\n", directorTruncate(setting, 500))
	}
	fmt.Fprintf(&b, "%s.\n%s\n", directorCameraTerm(sh.Camera), directorShotAction(d, sh))
	if len(cast) > 0 {
		b.WriteString("Characters in this frame (all must appear, each exactly once, placed inside the environment):\n")
		for i, ch := range cast {
			ref := ""
			if i < referenced {
				ref = fmt.Sprintf(" — same person as reference Image %d: identical face and facial features, skin tone, body shape and height, hairstyle and outfit", i+1)
			}
			fmt.Fprintf(&b, "- %s: %s%s. Wears exactly this outfit, same colors and accessories, in every shot.\n", directorCharacterName(ch, i), directorTruncate(ch.Description, 600), ref)
		}
		b.WriteString("Never change a named character's face, body, clothing, colors, hairstyle or accessories between shots, even if another line mentions clothing.\n")
		if referenced > 0 {
			b.WriteString("Reference images supply identity only: copy faces, body proportions, hair and outfits exactly; do not copy their art style, rendering, color grading, lighting, background or pose. Re-render the characters in the art style above.\n")
		}
	} else {
		b.WriteString("Do not add named main characters that are not described.\n")
	}
	b.WriteString(directorPropLines(d, directorShotAction(d, sh)))
	if bible := strings.TrimSpace(d.Bible); bible != "" {
		fmt.Fprintf(&b, "World and production design: %s\n", directorTruncate(bible, 500))
	}
	mood := strings.Join(p.Idea.Moods, ", ")
	if sc != nil && sc.Mood != nil {
		mood = *sc.Mood
	}
	fmt.Fprintf(&b, "Genre: %s. Mood: %s. Aspect ratio %s.\n", p.Idea.Genre, mood, p.AspectRatio)
	if setting != "" {
		fmt.Fprintf(&b, "Background must show this location: %s. ", directorTruncate(setting, 160))
	}
	b.WriteString("Detailed, story-appropriate background with depth; never a plain, blank, solid-color, gradient or studio backdrop. No text, captions, logos or watermarks. Anatomically correct hands and faces. ")
	fmt.Fprintf(&b, "Final image style: %s.", directorStyleShort(p))
	return directorTruncate(b.String(), directorPromptLimit)
}

// Continuity starts from the actual preceding clip, rather than an independently generated storyboard image.
func directorVideoPrompt(d *directorDocument, p *dto.DirectorProject, sh *dto.DirectorShot, cast []directorCharacter, continuity bool) string {
	sc := directorFindScene(p, sh.SceneID)
	var b strings.Builder
	b.WriteString("Image 1 is the exact first frame of this clip: start on it unchanged, same composition, characters and background.\n")
	if continuity {
		b.WriteString("Image 1 is the actual final frame of the preceding video. Continue its motion naturally without a jump or reset.\n")
	}
	if len(cast) > 0 {
		descriptions := []string{}
		for i, ch := range cast {
			descriptions = append(descriptions, fmt.Sprintf("%s (%s)", directorCharacterName(ch, i), directorTruncate(ch.Description, 200)))
		}
		fmt.Fprintf(&b, "Character descriptions: %s. Preserve the visible characters' identity and clothing from the supplied starting frame. Do not add or remove characters.\n", strings.Join(descriptions, "; "))
	}
	fmt.Fprintf(&b, "Shot: %s. Camera movement: %s.\n", directorCameraTerm(sh.Camera), directorCameraMotionTerm(sh.Camera))
	fmt.Fprintf(&b, "Action: %s\n", directorShotAction(d, sh))
	b.WriteString(directorPropLines(d, directorShotAction(d, sh)))
	if setting := directorSceneSetting(d, sc); setting != "" {
		fmt.Fprintf(&b, "Setting: %s\n", directorTruncate(setting, 400))
	}
	fmt.Fprintf(&b, "Art style: %s, matching Image 1. Keep the background, lighting and color grade of Image 1. Smooth natural motion, single continuous shot, no scene cuts, no text overlays.", directorStyleShort(p))
	return b.String()
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

func directorCoverPrompt(p *dto.DirectorProject, cast []directorCharacter, referenced int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Key art poster for a %s story: %s\n", p.Idea.Genre, p.Idea.Prompt)
	for i, ch := range cast {
		ref := ""
		if i < referenced {
			ref = fmt.Sprintf(" — identity from reference Image %d", i+1)
		}
		fmt.Fprintf(&b, "- %s: %s%s\n", directorCharacterName(ch, i), directorTruncate(ch.Description, 500), ref)
	}
	if len(cast) > 0 {
		b.WriteString("Never change a named character's clothing, colors, hairstyle or accessories from the description above.\n")
	}
	if referenced > 0 {
		b.WriteString("Reference images define character identity only; ignore their backgrounds.\n")
	}
	fmt.Fprintf(&b, "Art style (mandatory, overrides the style of any reference image): %s. Mood: %s. Aspect ratio %s. Rich story-world background, strong focal composition, no text, titles, logos or watermarks.", directorStyle(p), strings.Join(p.Idea.Moods, ", "), p.AspectRatio)
	return directorTruncate(b.String(), directorPromptLimit)
}

// Library characters often share a default name; duplicates get a letter so the planner and
// the image prompt can tell them apart ("ตัวละคร A", "ตัวละคร B").
func directorUniqueNames(chars []directorCharacter) {
	count := map[string]int{}
	for i := range chars {
		chars[i].Name = strings.TrimSpace(chars[i].Name)
		count[chars[i].Name]++
	}
	letter := map[string]int{}
	for i := range chars {
		name := chars[i].Name
		if name == "" || count[name] > 1 {
			base := name
			if base == "" {
				base = "ตัวละคร"
			}
			chars[i].Name = fmt.Sprintf("%s %c", base, 'A'+rune(letter[name]))
			letter[name]++
		}
	}
}

// directorFeaturedCast narrows a shot's cast to characters named in its text, ordered by first
// mention. Planners often list every scene character on every shot; with a single reference
// image the first listed character would otherwise replace the one the shot is about.
func directorFeaturedCast(chars []directorCharacter, text string) []directorCharacter {
	type hit struct {
		ch  directorCharacter
		pos int
	}
	hits := []hit{}
	masked := text
	order := append([]directorCharacter{}, chars...)
	// Longer names first so "บักจ่อย" is not consumed by a shorter overlapping name.
	sort.SliceStable(order, func(i, j int) bool { return len([]rune(order[i].Name)) > len([]rune(order[j].Name)) })
	for _, ch := range order {
		name := strings.TrimSpace(ch.Name)
		if name == "" {
			continue
		}
		if pos := strings.Index(masked, name); pos >= 0 {
			hits = append(hits, hit{ch, pos})
			masked = strings.ReplaceAll(masked, name, strings.Repeat(" ", len(name)))
		}
	}
	if len(hits) == 0 {
		return chars
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })
	out := make([]directorCharacter, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ch)
	}
	return out
}

func directorExtensionPrompt(d *directorDocument, p *dto.DirectorProject, sh *dto.DirectorShot, cast []directorCharacter, seconds int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Extend Video1 forward by %d seconds. Generate ONLY new footage after Video1's final frame. Do not repeat, include or replay the input video. Continue the exact ongoing motion, velocity, direction, character poses, expression, camera trajectory, background and lighting. No freeze, pause, reset or new establishing shot.\n", seconds)
	fmt.Fprintf(&b, "Continue into this action: %s\nCamera movement: %s\n", directorShotAction(d, sh), directorCameraMotionTerm(sh.Camera))
	b.WriteString(directorPropLines(d, directorShotAction(d, sh)))
	for i, ch := range cast {
		fmt.Fprintf(&b, "Character: %s (%s). Preserve the visible identity and clothing from Video1.\n", directorCharacterName(ch, i), directorTruncate(ch.Description, 200))
	}
	return b.String()
}

// directorTextOnlyVideoPrompt is experiment A3: a realistic/cinematic scene opening generated from
// text alone, so no reference image can be rejected or leak a CG look.
func directorTextOnlyVideoPrompt(d *directorDocument, p *dto.DirectorProject, sh *dto.DirectorShot, cast []directorCharacter) string {
	sc := directorFindScene(p, sh.SceneID)
	var b strings.Builder
	fmt.Fprintf(&b, "Live-action film footage: %s. Real human actors, real costumes, real materials and practical lighting; not CGI, not 3D animation, not a game render, not illustration.\n", directorStyle(p))
	fmt.Fprintf(&b, "Shot: %s. Camera movement: %s.\n", directorCameraTerm(sh.Camera), directorCameraMotionTerm(sh.Camera))
	fmt.Fprintf(&b, "Action: %s\n", directorShotAction(d, sh))
	if setting := directorSceneSetting(d, sc); setting != "" {
		fmt.Fprintf(&b, "Setting: %s\n", directorTruncate(setting, 400))
	}
	for i, ch := range cast {
		fmt.Fprintf(&b, "Character %s: %s. Exactly this face, hair and outfit.\n", directorCharacterName(ch, i), directorTruncate(ch.Description, 600))
	}
	b.WriteString(directorPropLines(d, directorShotAction(d, sh)))
	if bible := strings.TrimSpace(d.Bible); bible != "" {
		fmt.Fprintf(&b, "World and production design: %s\n", directorTruncate(bible, 500))
	}
	b.WriteString("Single continuous shot, natural motion, no scene cuts, no text overlays.")
	return b.String()
}
