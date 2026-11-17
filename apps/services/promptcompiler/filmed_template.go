package promptcompiler

import (
	"fmt"
	"strings"
)

const FilmedTemplateVersion = "director-user-filmed-template-v1"

// FilmedLayoutVersion places shot context between the user's mode block and the
// negative constraints, so the provider cannot read the shot as part of the negative list.
const FilmedLayoutVersion = "director-filmed-layout-v4"

// FilmedPoseCastingVersion identifies the characters-only reference control used by Director.
const FilmedPoseCastingVersion = "director-pose-casting-v1"

// filmedHumanControl is the studio pipeline's live-action control, shared verbatim with Director pose casting.
const filmedHumanControl = "Filmed live-action human actor look: anatomically plausible skin with subtle texture and translucency, individual hair and real cloth. Natural eyes, blinks, expressions, spoken lip sync and grounded motion. Motivated practical lighting, natural shadows, optical depth and subtle shutter motion blur. Preserve designed identity as a filmed person, not CG/cartoon/plastic/doll; follow scene framing."

// filmedLead precedes the verbatim user template: Seedance weights the opening instruction,
// and the synthetic reference is otherwise copied as a rendering target.
const filmedLead = "Output live-action film footage with real human performers, not CGI or animation. The reference image is stylized CG concept art used only for character design, layout and composition: cast each character as a real person with the same apparent age, build, hairstyle and outfit, and replace all stylized faces, skin, hair and material rendering with naturally photographed detail."

const filmedBaseTemplate = `Image 1 is the primary reference for character identity, visual composition, and scene structure, not a target for reproducing CGI rendering or synthetic surface materials.

Reconstruct the entire scene as authentic live-action footage captured in the physical world with real human actors, real environments, real objects, and physically believable materials.

**Character and Identity Preservation**

Preserve the character's recognizable facial identity, apparent age, natural body proportions, hairstyle silhouette, clothing design, posture, spatial placement, and relationship to the surrounding environment. Maintain consistent facial geometry, anatomy, and identity throughout every frame. Do not reinterpret, beautify, stylize, or redesign the character.

**Human Photorealism**

Render human skin with naturally occurring microtexture, subtle pores, fine facial hair, realistic pigmentation variations, and physically accurate light scattering. Skin must respond naturally to lighting, with soft diffuse reflection, restrained localized highlights, and realistic transitions between illuminated and shadowed areas.

Eyes must have anatomically realistic structure, natural moisture, accurate reflections, stable gaze direction, subtle eye movements, and spontaneous, non-mechanical blinking.

Hair must consist of fine, individually distinguishable strands with realistic density, natural layering, varied thickness, subtle flyaways, and physically believable movement. Preserve the original hairstyle while replacing artificial clumps or sculpted CGI hair with naturally photographed hair.

Facial expressions must emerge through subtle, coordinated muscle movement. Include natural breathing, small posture adjustments, realistic weight distribution, slight asymmetry, and organic timing. Avoid exaggerated expressions, mechanical motion, or synchronized movements.

**Physical Materials and Environment**

Every visible object must exhibit physically plausible material properties, including appropriate texture, roughness, reflectivity, translucency, surface imperfections, and interaction with surrounding light.

Clothing must have realistic fabric fibers, thickness, seams, wrinkles, gravity-driven folds, and movement consistent with the character's actions.

The environment must feel physically inhabited and naturally photographed, with coherent spatial depth, believable scale, accurate contact shadows, realistic atmospheric conditions, and consistent lighting across all surfaces.

**Motion and Temporal Consistency**

All motion must follow natural human biomechanics, realistic gravity, inertia, momentum, and material behavior.

Maintain stable character identity, body proportions, object geometry, clothing details, and environmental continuity throughout the entire sequence.

Preserve consistent lighting, shadows, reflections, perspective, and spatial relationships as the camera or subjects move.

Avoid temporal flickering, texture swimming, facial morphing, anatomical distortion, unnatural acceleration, object deformation, or inconsistent physical interactions.

**Camera and Image Formation**

The final result must resemble footage captured by a real optical camera, not a computer-generated animation.

Use physically plausible perspective, natural optical behavior, realistic exposure, coherent motion blur, and appropriate depth of field.

Preserve the reference image's broad framing, composition, environment, and time of day unless camera movement or scene changes are explicitly requested.

Prioritize authentic physical reality over visual perfection. Natural imperfections, subtle asymmetry, and believable material responses are essential.

The final footage must be visually indistinguishable in intent from real-world live-action cinematography, with no visible CGI, game-engine, plastic, waxy, cartoon, or synthetic rendering characteristics.`

const filmedRealisticTemplate = `**REALISTIC MODE — AUTHENTIC OBSERVATIONAL FOOTAGE**

Apply uncompromising natural photographic realism to the entire scene.

Capture the environment as if recorded spontaneously by a real camera observing actual people and physical events.

Use naturally available lighting, realistic ambient illumination, physically accurate shadow softness, and exposure consistent with the location, weather, and time of day.

Maintain neutral, believable white balance and natural color reproduction. Preserve realistic skin tones, environmental colors, and material appearance without stylized color grading.

Camera behavior should feel observational and human-operated, with subtle, physically plausible movement when appropriate. Avoid unnecessary camera movement, artificial stabilization effects, dramatic framing, or cinematic exaggeration.

Preserve natural optical sharpness, realistic dynamic range, restrained sensor characteristics, and depth of field consistent with the actual camera distance and lens.

Human movement must feel spontaneous and unperformed, including naturally imperfect timing, subtle breathing, involuntary micro-movements, and believable interaction with the environment.

Do not introduce beauty lighting, artificial skin smoothing, excessive sharpening, exaggerated texture, heavy contrast, synthetic bokeh, or stylized visual effects.

The entire sequence should resemble authentic, unretouched, high-quality live-action documentary footage.`

const filmedCinematicTemplate = `**CINEMATIC MODE — PHOTOREALISTIC LIVE-ACTION CINEMATOGRAPHY**

Apply premium cinematic photographic realism while maintaining absolute physical believability.

The scene must look like footage captured on a professional cinema camera with real actors, practical environments, authentic materials, and carefully controlled cinematography.

Use motivated cinematic lighting derived from believable sources within or outside the scene. Shape light naturally through realistic key, fill, bounce, and practical illumination.

Maintain physically accurate light falloff, subtle atmospheric depth, coherent shadow direction, natural skin reflectance, and controlled highlight rolloff.

Apply restrained, filmic color science with realistic skin tones, balanced contrast, rich but believable tonal separation, and scene-consistent color grading.

Use physically plausible lens characteristics, natural focus transitions, appropriate optical depth of field, and motion blur consistent with real camera shutter behavior.

Camera movement must feel deliberate, smooth, and mechanically achievable through real cinematography techniques, without impossible trajectories or synthetic floating-camera motion.

Human performances must remain natural, emotionally convincing, and anatomically precise. Preserve subtle facial expressions, organic body language, realistic eye behavior, and physically grounded interactions.

Maintain fine photographic detail in skin, hair, fabrics, architecture, and environmental surfaces without artificial sharpening or excessive texture enhancement.

Preserve the scene's original time of day, weather, spatial structure, and lighting logic.

Avoid excessive bloom, artificial lens flares, forced shallow depth of field, aggressive teal-orange grading, exaggerated contrast, glossy CGI surfaces, or stylized visual effects.

The final result should resemble a professionally photographed, high-budget live-action film scene rather than a rendered cinematic animation.`

const filmedNegativeTemplate = `CGI appearance, 3D render, game engine graphics, Unreal Engine look, synthetic materials, cartoon, anime, illustration, plastic skin, waxy skin, rubber skin, porcelain skin, beauty filter, airbrushed face, artificial skin smoothing, exaggerated pores, excessive gloss, unnatural subsurface scattering, doll-like face, mannequin appearance, glassy eyes, lifeless eyes, artificial hair clumps, helmet hair, rigid hair, synthetic fabric, floating objects, unrealistic gravity, robotic movement, stiff facial animation, mechanical blinking, unnatural body proportions, anatomical distortion, deformed hands, extra fingers, facial morphing, identity drift, inconsistent facial features, temporal flickering, texture swimming, unstable geometry, inconsistent shadows, incorrect reflections, artificial depth of field, excessive bokeh, overprocessed HDR, oversharpening, excessive bloom, artificial lens flare, oversaturated colors, exaggerated cinematic grading, unnatural camera movement, impossible perspective, inconsistent lighting.`

var filmedNegativeSection = "Negative constraints:\n" + filmedNegativeTemplate

func filmedTemplate(look FilmedLook, imageIndex int) string {
	return filmedTemplateHead(look, imageIndex) + "\n\n" + filmedNegativeSection
}

func filmedTemplateHead(look FilmedLook, imageIndex int) string {
	base := filmedBaseTemplate
	if imageIndex != 1 {
		base = strings.ReplaceAll(base, "Image 1", fmt.Sprintf("Image %d", imageIndex))
	}
	mode := filmedRealisticTemplate
	if look == FilmedLookCinematic {
		mode = filmedCinematicTemplate
	}
	return base + "\n\n" + mode
}

// composeFilmedPrompt returns false when no filmed template control is present.
func composeFilmedPrompt(controls, sections []string) (string, bool) {
	for i, c := range controls {
		head, ok := strings.CutSuffix(c, "\n\n"+filmedNegativeSection)
		if !ok {
			continue
		}
		rest := append(append([]string{}, controls[:i]...), controls[i+1:]...)
		shot := strings.Join(append(rest, sections...), "\n")
		if strings.TrimSpace(shot) == "" {
			return filmedLead + "\n\n" + head + "\n\n" + filmedNegativeSection, true
		}
		return filmedLead + "\n\n" + head + "\n\nShot:\n" + shot + "\n\n" + filmedNegativeSection, true
	}
	return "", false
}

// filmedPoseCastingTemplate mirrors the studio casting control, adds pose, and keeps the user's
// mode and negative blocks; the base block is omitted because it asks to preserve the reference
// environment, which a neutral-background casting image does not have.
func filmedPoseCastingTemplate(look FilmedLook, imageIndex int) string {
	mode := filmedRealisticTemplate
	if look == FilmedLookCinematic {
		mode = filmedCinematicTemplate
	}
	casting := fmt.Sprintf("Image %d: casting and pose guide shown on a neutral studio background. Use only each character's face and body geometry, haircut, outfit, worn or held props, pose, gesture, expression and relative placement; no beautification or identity drift. Exclude its shading, CG materials, drawn skin, lighting and gray background. Not a texture, style or start frame: place the characters in the scene environment described in the shot and follow its action and camera.", imageIndex)
	return casting + "\n\n" + filmedHumanControl + "\n\n" + mode + "\n\n" + filmedNegativeSection
}
