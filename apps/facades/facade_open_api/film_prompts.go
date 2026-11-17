package facade_open_api

import (
	"fmt"
	"strings"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
)

// Film prompt builders. They call Director's read-only helpers (style terms, integrity rules,
// filmed neutralizer) but never change Director prompts.

const filmFilmedLead = "Live-action photorealistic film footage shot on a cinema camera, natural skin texture and real fabrics. " +
	"Reference images of people are costume and body references whose faces are hidden on purpose: render every face, skin and hair photorealistically from the character descriptions; never copy blur, gray or featureless surfaces into the video."

type filmBrief struct {
	Shot *filmShot
	// InFrame are characters visible in the previous shot's last frame who are not in this shot's
	// cast; a continuation starts on that frame, so they must be named or the model invents roles.
	InFrame  []*models.FilmElement
	Cast     []*models.FilmElement
	Location *models.FilmElement
	Props    []*models.FilmElement
	Style    string
	Filmed   bool
}

// filmText removes art-style words from user text in realistic/cinematic mode (same rule as Director).
func filmText(b *filmBrief, s string) string {
	s = strings.TrimSpace(s)
	if b.Filmed {
		return strings.TrimSpace(directorFilmedNeutralize(s))
	}
	return s
}

func filmStyleLine(b *filmBrief) string {
	if b.Filmed {
		return filmFilmedLead
	}
	return fmt.Sprintf("Art style (mandatory for the whole clip, overrides the rendering of any reference image): %s.", directorStyle(&dto.DirectorProject{Style: b.Style}))
}

func filmCharacterName(el *models.FilmElement) string { return "@" + el.Name }

// filmReferences lists the reference images in prompt order: characters, location, props.
func filmReferences(b *filmBrief) []filmRef {
	refs := []filmRef{}
	for _, el := range b.Cast {
		refs = append(refs, filmRef{Key: filmReferenceKey(el, b.Filmed), Label: filmCharacterName(el)})
	}
	if b.Location != nil {
		refs = append(refs, filmRef{Key: filmReferenceKey(b.Location, b.Filmed), Label: "#" + b.Location.Name})
	}
	for _, el := range b.Props {
		refs = append(refs, filmRef{Key: filmReferenceKey(el, b.Filmed), Label: el.Name})
	}
	return refs
}

func filmPerformance(b *filmBrief, w *strings.Builder) {
	sh := b.Shot
	fmt.Fprintf(w, "Action: %s\n", filmText(b, sh.Action))
	if cam := filmText(b, sh.Camera); cam != "" {
		fmt.Fprintf(w, "Camera: %s\n", cam)
	}
	names := map[string]string{}
	for _, el := range b.Cast {
		names[el.ID.String()] = filmCharacterName(el)
	}
	spoke := map[string]bool{}
	if len(sh.Dialogue) > 0 || sh.Audio != "" {
		w.WriteString("[AUDIO]:\n")
	}
	for _, l := range sh.Dialogue {
		spoke[l.SpeakerID] = true
		delivery := ""
		if l.Delivery != "" {
			delivery = ", " + filmText(b, l.Delivery)
		}
		fmt.Fprintf(w, "- %s speaks in Thai%s: \"%s\"\n", names[l.SpeakerID], delivery, l.Line)
	}
	if len(sh.Dialogue) > 0 {
		for _, el := range b.Cast {
			if !spoke[el.ID.String()] {
				fmt.Fprintf(w, "- No speech from %s.\n", filmCharacterName(el))
			}
		}
	}
	if audio := filmText(b, sh.Audio); audio != "" {
		fmt.Fprintf(w, "- Sound: %s\n", audio)
	}
	if len(sh.Dialogue) == 0 {
		w.WriteString("No spoken dialogue.\n")
	}
	if len(b.Props) > 0 {
		w.WriteString(directorObjectIntegrity + "\n")
	}
	if len(b.Cast) > 0 {
		w.WriteString(directorFaceIntegrity + "\n")
	}
	w.WriteString("Single continuous shot, no scene cuts, no text overlays, no subtitles, no watermarks.")
}

// filmReferencePrompt builds a reference-to-video prompt: every reference image is labelled with
// the element it shows, then the shot's action, camera and audio.
func filmReferencePrompt(b *filmBrief) string {
	var w strings.Builder
	w.WriteString(filmStyleLine(b) + "\n")
	n := 0
	w.WriteString("Reference images:\n")
	for _, el := range b.Cast {
		n++
		fmt.Fprintf(&w, "- Image %d is the character %s: %s. Same face, body, hair and outfit in every frame; appears exactly once.\n", n, filmCharacterName(el), directorTruncate(filmText(b, el.Spec), 600))
	}
	if el := b.Location; el != nil {
		n++
		fmt.Fprintf(&w, "- Image %d is the location #%s: %s. The whole clip takes place here; keep its architecture, layout and lighting.\n", n, el.Name, directorTruncate(filmText(b, el.Spec), 500))
	}
	for _, el := range b.Props {
		n++
		fmt.Fprintf(&w, "- Image %d is the prop %s: %s. Exact same structure, materials and size.\n", n, el.Name, directorTruncate(filmText(b, el.Spec), 300))
	}
	if len(b.Cast) > 0 {
		w.WriteString("Do not add other main characters.\n")
	}
	filmPerformance(b, &w)
	return w.String()
}

// filmContinuePrompt starts on the previous clip's last frame (first_frame), so references are
// given as text only: the provider cannot mix first-frame and reference images.
func filmContinuePrompt(b *filmBrief) string {
	var w strings.Builder
	w.WriteString("Image 1 is the exact first frame of this clip and the final frame of the previous shot: start on it unchanged and continue its motion naturally, without a jump, reset or new establishing shot. Keep the camera's existing direction and speed constant from the first frame (no ease-in, no stop, no new camera move), unless the camera line below says static.\n")
	w.WriteString(filmStyleLine(b) + "\n")
	for _, el := range b.Cast {
		fmt.Fprintf(&w, "Character %s: %s. Keep the identity and outfit visible in Image 1.\n", filmCharacterName(el), directorTruncate(filmText(b, el.Spec), 600))
	}
	for _, el := range b.InFrame {
		fmt.Fprintf(&w, "%s is also in Image 1: %s. Same person and side as before; he stays in the background or leaves frame and never fights or swaps roles with the listed characters.\n", filmCharacterName(el), directorTruncate(filmText(b, el.Spec), 300))
	}
	if el := b.Location; el != nil {
		fmt.Fprintf(&w, "Location #%s: %s.\n", el.Name, directorTruncate(filmText(b, el.Spec), 500))
	}
	for _, el := range b.Props {
		fmt.Fprintf(&w, "Prop %s: %s.\n", el.Name, directorTruncate(filmText(b, el.Spec), 300))
	}
	filmPerformance(b, &w)
	return w.String()
}

func filmValidatePrompt(p string) error { return promptcompiler.ValidateVideoPrompt(p) }

// filmElementPrompt is the reference image prompt of one element.
func filmElementPrompt(in filmElementJob) string {
	b := &filmBrief{Style: in.Style, Filmed: in.Filmed}
	style := directorStyleTerms[in.Style]
	if style == "" {
		style = in.Style
	}
	spec := filmText(b, in.Spec)
	switch in.Kind {
	case "character":
		return fmt.Sprintf("Art style: %s.\nFull-body character reference of %s: %s.\nOne person only, standing in a relaxed neutral pose facing the camera, whole body visible from head to toe, arms slightly away from the body, face clearly visible. Plain light-gray studio background, soft even lighting, no props in hand unless described. No text, logos or watermarks.", style, in.Name, spec)
	case "location":
		return fmt.Sprintf("Art style: %s.\nEstablishing reference plate of the location %s: %s.\nWide shot of the empty environment with depth and clear layout, no people, no characters, no animals in focus. Lighting and weather as described. No text, logos or watermarks.", style, in.Name, spec)
	default:
		return fmt.Sprintf("Art style: %s.\nReference image of the prop %s: %s.\nThe single object alone, centered, fully visible, plain light-gray background, soft even lighting. No hands, no people, no text, logos or watermarks. %s", style, in.Name, spec, directorObjectIntegrity)
	}
}

// filmCastingPrompt turns a realistic character portrait into the faceless costume reference that
// the video provider accepts (Director's faceless casting rule, applied to one element).
func filmCastingPrompt(in filmElementJob) string {
	b := &filmBrief{Style: in.Style, Filmed: true}
	return characterdesign.CompileDirectorFacelessCasting(in.Name + ": " + filmText(b, in.Spec))
}
