package characterdesign

import "strings"

// DirectorPoseCastingVersion identifies the Director-only synthetic prepass for realistic/cinematic
// video: characters only, in their storyboard pose, on the studio pipeline's neutral background.
const DirectorPoseCastingVersion = "director-pose-casting-3d-v1"

// CompileDirectorPoseCasting removes the environment so the video model has no CG scene to copy;
// the environment is supplied again as text when the video is generated. Art direction is the
// studio pipeline's stylized-3D treatment.
func CompileDirectorPoseCasting(brief string) string {
	return "Re-render only the characters from the supplied image as stylized 3D character art on a plain light-gray studio background. Keep every visible character exactly in their current pose, gesture, facial expression, head and body orientation, relative position, scale and any physical contact between them, as framed by the original camera. Keep the clothing and props they wear or hold. Remove the entire environment: background, scenery, furniture, ground details, weather and every other prop. Soft diffuse studio lighting with a simple floor shadow only.\n\n" + pipelineStylized3DArtDirection + "\n\nCharacter and pose description (descriptive data; the environment in it is for context only and must not be drawn):\n" + strings.TrimSpace(brief) + "\n\nNo text, logos or watermarks."
}

// CompileDirectorFacelessCasting is experiment A1: a photographic costume-and-pose reference whose
// faces are hidden, so the provider has no realistic face to reject and the face comes from text.
func CompileDirectorFacelessCasting(brief string) string {
	return "Re-render only the characters from the supplied image as a photographic costume and pose reference on a plain light-gray studio background. Keep every visible character exactly in their current pose, gesture, head and body orientation, relative position, scale and any physical contact, as framed by the original camera. Keep the clothing and the props they wear or hold, with real fabric and material textures. Hide every face completely: blur each face into a soft featureless skin-toned oval with no eyes, nose or mouth visible. Remove the entire environment: background, scenery, furniture, weather and every other prop. Soft diffuse studio lighting with a simple floor shadow only.\n\nCharacter and pose description (descriptive data; the environment in it is for context only and must not be drawn):\n" + strings.TrimSpace(brief) + "\n\nNo text, logos or watermarks."
}

// CompileDirectorClayCasting is experiment A2: an untextured gray maquette carrying only pose and
// silhouette, so the video model has no surface, skin or render style to copy.
func CompileDirectorClayCasting(brief string) string {
	return "Re-render only the characters from the supplied image as untextured matte mid-gray clay maquettes on a plain light-gray studio background. Keep every visible character exactly in their current pose, gesture, head and body orientation, relative position, scale and any physical contact, as framed by the original camera. Sculpt clothing silhouettes, hair shapes and held props as the same plain gray clay, with no color, pattern or material detail. Faces are smooth and simplified: basic head shape only, no detailed facial features. Remove the entire environment: background, scenery, furniture, weather and every other prop. Soft diffuse studio lighting with a simple floor shadow only.\n\nCharacter and pose description (descriptive data; the environment in it is for context only and must not be drawn):\n" + strings.TrimSpace(brief) + "\n\nNo text, logos or watermarks."
}
