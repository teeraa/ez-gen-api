package productdesign

import (
	"errors"
	"strings"
)

const CompilerVersion = "product-design-v1"

type Options struct {
	VisualStyle string `json:"visualStyle"`
	Mood        string `json:"mood"`
}

var styles = map[string]string{
	"auto":                "Respect the requested product materials and visual style; default to clear studio product photography.",
	"cinematic_realistic": "Realistic product photography with accurate material texture and readable packaging details.",
	"3d_animation":        "Stylized 3D product rendering with coherent geometry and clear material definition.",
	"anime":               "Anime-inspired product illustration with clear edges and readable product geometry.",
	"illustration":        "Product illustration with controlled line work, consistent shapes and clear color separation.",
	"clay":                "Tactile clay-style product rendering while keeping the product shape and packaging recognizable.",
}
var moods = map[string]string{
	"neutral":    "Neutral, balanced product presentation.",
	"warm":       "Warm color treatment and soft inviting product lighting.",
	"confident":  "Confident product presentation with crisp readable edges.",
	"mysterious": "Subtle mysterious color treatment while keeping all product details clearly visible.",
	"serious":    "Restrained professional product color treatment.",
	"energetic":  "Lively product color treatment while keeping the product stationary and clearly readable.",
}

func Normalize(o Options) (Options, error) {
	if o.VisualStyle == "" {
		o.VisualStyle = "auto"
	}
	if o.Mood == "" {
		o.Mood = "neutral"
	}
	if _, ok := styles[o.VisualStyle]; !ok {
		return Options{}, errors.New("invalid visual style")
	}
	if _, ok := moods[o.Mood]; !ok {
		return Options{}, errors.New("invalid mood")
	}
	return o, nil
}
func Compile(brief string, o Options, reference bool) (string, error) {
	o, err := Normalize(o)
	if err != nil {
		return "", err
	}
	sections := []string{"Create one single product packshot for a reusable production reference.", "Product brief:\n" + strings.TrimSpace(brief), styles[o.VisualStyle], moods[o.Mood], "Show one complete product and its specified packaging, centered with generous margins, on a plain neutral background with soft even studio lighting. Keep proportions, materials, recognizable package shape and existing label details accurate. No extra views, panels, duplicate products, props or watermarks. Do not invent, add or replace brand names, logos, typography, label text, certifications or marketing claims. Product recognition and label fidelity take priority over style treatment and scene staging."}
	if reference {
		sections = append(sections, "Preserve the supplied reference product, packaging, colors, recognizable details and existing labels. Do not redesign the product or add unsupported brand details.")
	}
	return strings.Join(sections, "\n\n"), nil
}
