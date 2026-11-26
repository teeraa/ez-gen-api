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

const filmFilmedLead = "Live-action feature-film footage, indistinguishable from a real movie shot on a cinema camera with spherical prime lenses: " +
	"shallow depth of field with natural focus fall-off, motivated light from real sources (window daylight, practical lamps, overcast sky) with soft shadows and dark corners, " +
	"muted low-saturation color grade with lifted blacks and gentle highlight roll-off, fine 35mm film grain, natural skin texture with pores and blemishes, real lived-in fabrics and sets, restrained naturalistic acting. " +
	"Reference images of people are costume and body references whose faces are hidden on purpose: render every face, skin and hair photorealistically from the character descriptions; never copy blur, gray or featureless surfaces into the video. " +
	"Reference images show identity, costume and place only: do not copy their pose, framing, plain backdrop or flat studio lighting."

// filmFilmedAvoid lists the looks that give generated live action away; it closes filmed prompts.
const filmFilmedAvoid = "Avoid: centered symmetrical framing, head-on frontal camera angles, people staring into the lens, glossy HDR or over-sharpened look, plastic or airbrushed skin, saturated teal-and-orange, flat even lighting, deep focus everywhere, CG or video-game look, posed frozen people, slow-motion or floaty fighting, held sword-locks, fighters gliding or sliding toward each other, a pose held before a cut, synthetic or echoing metal clangs, a centred frontal close-up posing in front of a scenic backdrop, a smirk or stare into the lens."

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
	// Dubbed are speakers whose lines are dubbed with their bound voice after the clip returns.
	Dubbed map[string]bool
	// AudioRef: the dubbed lines are sent as reference audio ("audio 1") so lips follow them.
	AudioRef bool
	// Native (with AudioRef): each speaker's audio is only a voice sample; the provider speaks the
	// lines itself and its audio is kept.
	Native bool
}

// filmNativeSpeakers lists dubbed speakers in order of their first line: speaker n uses audio n.
func filmNativeSpeakers(sh *filmShot, dubbed map[string]bool) []string {
	out, seen := []string{}, map[string]bool{}
	for _, l := range sh.Dialogue {
		if dubbed[l.SpeakerID] && !seen[l.SpeakerID] {
			seen[l.SpeakerID] = true
			out = append(out, l.SpeakerID)
		}
	}
	return out
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

// filmLockedCamera pins the framing of a shot written as a static camera: video models otherwise
// add a slow push in or drift on their own.
const filmLockedCamera = "Camera locked off on a tripod for the whole shot: no zoom, no push in or pull out, no dolly, pan, tilt, orbit or handheld drift; the framing stays exactly the same from the first frame to the last, only the people and things in the scene move."

// filmStaticCamera reports whether a shot's camera line asks for a fixed camera.
func filmStaticCamera(camera string) bool {
	c := strings.ToLower(camera)
	return strings.Contains(c, "static") || strings.Contains(c, "locked-off") || strings.Contains(c, "locked off") || strings.Contains(c, "tripod")
}

// filmSequence writes a multi-shot clip: its numbered shots joined by hard cuts at real speed, so
// a trailer, fight or montage cuts every 1-3 s inside one generated clip.
func filmSequence(b *filmBrief) string {
	var w strings.Builder
	w.WriteString("MULTI-SHOT SEQUENCE (follow exactly, in this order): hard cuts between the shots below inside this one clip, each shot about the same length, real speed with no slow motion; every cut lands in the middle of a movement, never after a held pose. Each shot shows only what it describes.\n")
	for i, c := range b.Shot.Cuts {
		cam := filmText(b, c.Camera)
		label := fmt.Sprintf("Shot %d", i+1)
		if i > 0 {
			label += " (hard cut)"
		}
		lock := ""
		if filmStaticCamera(cam) {
			lock = " Camera locked off for this shot."
		}
		fmt.Fprintf(&w, "%s: %s %s%s\n", label, cam, filmText(b, c.Action), lock)
	}
	return w.String()
}

// filmShotLead puts the camera first in live-action prompts: the model weights the start of the
// prompt most, and frontal reference images otherwise pull every shot to a head-on portrait.
func filmShotLead(b *filmBrief) string {
	if b.Filmed && len(b.Shot.Cuts) > 0 {
		return filmSequence(b)
	}
	cam := filmText(b, b.Shot.Camera)
	if !b.Filmed || cam == "" {
		return ""
	}
	lock := ""
	if filmStaticCamera(cam) {
		lock = " " + filmLockedCamera
	}
	return "SHOT (follow exactly): " + cam + lock + " Keep this camera position and angle even though the reference images face the camera; faces are seen from the angle written here, not head-on.\n"
}

// filmSpeechStyle keeps dubbed speech looking filmed rather than generated: the video model
// otherwise opens the mouth wide on every syllable, turns hidden speakers to the lens and stages
// entrances or laughs while the line plays (the dub starts about half a second into the clip).
const filmSpeechStyle = `- Speech looks like a restrained film actor talking quietly: lips part only slightly, the jaw barely moves, teeth mostly hidden, no wide-open mouth, no over-enunciation, no big smile or laugh while talking, even when the voice is loud or excited.
- Every speaker is already in place and in frame in the first frame and starts talking within the first second: no entrance, walk-in, turn toward the camera, eating, drinking or laughing during their line; such actions happen only after the line ends.
- A speaker framed from behind, in profile or as the out-of-focus foreground shoulder stays exactly that way for the whole shot: never turn or move them to show their mouth to the camera.
`

// filmBlockingLines states where everyone is and how, near the top of the prompt: references are
// standing identity photos, and without it the model re-stages each cut (sides swap, a seated
// character stands up).
func filmBlockingLines(b *filmBrief) string {
	blocking := filmText(b, b.Shot.Blocking)
	if blocking == "" || len(b.Cast) == 0 {
		return ""
	}
	return "[Blocking] Keep these frame sides, postures and facing directions for the whole clip unless the action below changes them: " + blocking + "\nCharacter reference images are standing identity photos only: each character's posture and place in frame are the ones written in [Blocking], not the reference pose.\n"
}

// filmSpokenOnly closes every list of lines the video model voices itself: the model reads any
// text near a line aloud, and spoke an English delivery note ("Firm, low growl, ...") before the
// Thai line when the note sat in brackets beside it.
const filmSpokenOnly = "- The only words anyone speaks are the Thai lines inside the quotation marks, word for word. Every other word in this prompt, including all English text and every acting direction, is silent direction: never say it aloud. No English speech, no narration, no extra words before, between or after the lines; speech starts directly on the first Thai word.\n"

// filmSilentDirection writes a line's delivery note under it, marked as acting only.
func filmSilentDirection(b *filmBrief, w *strings.Builder, indent string, l filmLine) {
	if d := filmText(b, l.Delivery); d != "" {
		fmt.Fprintf(w, "%sActing direction for this line (performed, never spoken aloud): %s\n", indent, d)
	}
}

func filmPerformance(b *filmBrief, w *strings.Builder) {
	sh := b.Shot
	fmt.Fprintf(w, "Action: %s\n", filmText(b, sh.Action))
	if cam := filmText(b, sh.Camera); cam != "" && !b.Filmed {
		fmt.Fprintf(w, "Camera: %s\n", cam)
		if filmStaticCamera(cam) && len(sh.Cuts) == 0 {
			fmt.Fprintf(w, "%s\n", filmLockedCamera)
		}
	}
	if len(sh.Cuts) > 0 && !b.Filmed {
		w.WriteString(filmSequence(b))
	}
	names := map[string]string{}
	for _, el := range b.Cast {
		names[el.ID.String()] = filmCharacterName(el)
	}
	spoke := map[string]bool{}
	if len(sh.Dialogue) > 0 || sh.Audio != "" {
		w.WriteString("[AUDIO]:\n")
	}
	if b.AudioRef && b.Native && len(b.Dubbed) > 0 {
		// Native speech: the provider voices the lines itself, so lips and words come from one model.
		for n, id := range filmNativeSpeakers(sh, b.Dubbed) {
			fmt.Fprintf(w, "- Audio %d is only a voice sample of %s: %s speaks with exactly this voice timbre, age and accent; do not play Audio %d itself.\n", n+1, names[id], names[id], n+1)
		}
		w.WriteString("- Generate the dialogue as natural spoken Thai, strictly as written, in this order, with no added, removed or repeated words; each speaker's lips match their own words:\n")
		n := 0
		for _, l := range sh.Dialogue {
			if !b.Dubbed[l.SpeakerID] {
				continue
			}
			n++
			spoke[l.SpeakerID] = true
			fmt.Fprintf(w, "  %d. %s says in Thai: \"%s\"\n", n, names[l.SpeakerID], l.Line)
			filmSilentDirection(b, w, "     ", l)
		}
		w.WriteString(filmSpokenOnly)
		w.WriteString("- Facial expression and body language match the emotion of each line. No background music, no subtitles.\n")
	} else if b.AudioRef && len(b.Dubbed) > 0 {
		// Voice-first: audio 1 already holds every dubbed line at its time; the picture performs it.
		w.WriteString("- Audio 1 is the finished dialogue track of this shot. Lip-sync to audio 1: each speaker's lips move only while their own line plays in audio 1 and rest closed in the silences. Lines in audio 1, in order:\n")
		n := 0
		for _, l := range sh.Dialogue {
			if !b.Dubbed[l.SpeakerID] {
				continue
			}
			n++
			spoke[l.SpeakerID] = true
			delivery := ""
			if l.Delivery != "" {
				delivery = " (" + filmText(b, l.Delivery) + ")"
			}
			fmt.Fprintf(w, "  %d. %s says in Thai%s: \"%s\"\n", n, names[l.SpeakerID], delivery, l.Line)
		}
		w.WriteString("- Facial expression and body language match the emotion heard in audio 1. Do not add other speech.\n")
	}
	if len(b.Dubbed) > 0 {
		w.WriteString(filmSpeechStyle)
	}
	voiced := false
	for _, l := range sh.Dialogue {
		if b.AudioRef && b.Dubbed[l.SpeakerID] {
			continue
		}
		spoke[l.SpeakerID] = true
		if b.Dubbed[l.SpeakerID] {
			// The voice is added in post, so only the performance is generated here.
			delivery := ""
			if l.Delivery != "" {
				delivery = ", " + filmText(b, l.Delivery)
			}
			fmt.Fprintf(w, "- %s says in Thai%s: \"%s\" — lips move subtly with the words, but this line is dubbed later: generate NO voice for %s.\n", names[l.SpeakerID], delivery, l.Line, names[l.SpeakerID])
			continue
		}
		fmt.Fprintf(w, "- %s speaks in Thai: \"%s\"\n", names[l.SpeakerID], l.Line)
		filmSilentDirection(b, w, "  ", l)
		voiced = true
	}
	if voiced {
		w.WriteString(filmSpokenOnly)
	}
	if len(b.Dubbed) > 0 && !b.AudioRef {
		w.WriteString("- Audio contains only ambience, sound effects and music for the dubbed lines; no audible human speech from dubbed characters.\n")
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
	if len(b.Cast) > 0 {
		w.WriteString("[Continuity] Keep every character's identity, face, hairstyle, outfit, relative height, frame side and posture consistent with [Blocking] and the references; no added or missing characters.\n")
	}
	if b.Filmed {
		w.WriteString(filmFilmedAvoid + "\n")
	}
	w.WriteString("Single continuous shot, no scene cuts, no text overlays, no subtitles, no watermarks.")
}

// filmReferencePrompt builds a reference-to-video prompt: every reference image is labelled with
// the element it shows, then the shot's action, camera and audio.
func filmReferencePrompt(b *filmBrief) string {
	var w strings.Builder
	w.WriteString(filmShotLead(b))
	w.WriteString(filmBlockingLines(b))
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
	w.WriteString(filmShotLead(b))
	w.WriteString(filmBlockingLines(b))
	w.WriteString(filmStyleLine(b) + "\n")
	for _, el := range b.Cast {
		fmt.Fprintf(&w, "Character %s: %s. Keep the identity and outfit visible in Image 1.\n", filmCharacterName(el), directorTruncate(filmText(b, el.Spec), 600))
	}
	for _, el := range b.InFrame {
		fmt.Fprintf(&w, "%s is also in Image 1: %s. Same person, same side of the frame and same posture as before; this character stays in the background or leaves frame and never fights or swaps roles with the listed characters.\n", filmCharacterName(el), directorTruncate(filmText(b, el.Spec), 300))
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
		if in.Filmed {
			return fmt.Sprintf("Art style: %s.\nFull-body costume reference photo of %s: %s.\nOne real person standing relaxed, whole body visible from head to toe, arms slightly away from the body, face clearly visible. Plain light-gray backdrop, soft natural window light from one side, natural skin texture without retouching, real fabric with creases and wear, everyday believable clothing as described. No text, logos or watermarks.", style, in.Name, spec)
		}
		return fmt.Sprintf("Art style: %s.\nFull-body character reference of %s: %s.\nOne person only, standing in a relaxed neutral pose facing the camera, whole body visible from head to toe, arms slightly away from the body, face clearly visible. Plain light-gray studio background, soft even lighting, no props in hand unless described. No text, logos or watermarks.", style, in.Name, spec)
	case "location":
		if in.Filmed {
			// A film still, not a 3D plate: the video model copies the reference's light and texture.
			return fmt.Sprintf("Art style: %s.\nCinematic film still of the location %s: %s.\nPhotographed on location from a person's eye height with a 35mm lens, an empty moment with no people: real, slightly worn, lived-in details, motivated natural or practical light with soft shadows and dark corners, muted low-saturation grade, fine film grain, slight haze. Not a 3D render, no symmetrical one-point perspective, no neon or exposed-brick clichés unless described. No text, logos or watermarks.", style, in.Name, spec)
		}
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
