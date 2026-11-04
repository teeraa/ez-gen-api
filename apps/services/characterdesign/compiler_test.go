package characterdesign

import (
	"strings"
	"testing"
)

func TestCompilerPreservesBriefAndReferenceAcrossDesignFormats(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ format, want string }{{"full_body", "both hands and both feet visible"}, {"turnaround", "exactly three consistent full-body views"}, {"portrait", "head-and-shoulders"}} {
		t.Run(tc.format, func(t *testing.T) {
			brief := "ตัวละครผมเงิน มีรอยแผลที่คิ้ว ใส่เสื้อคลุมสีแดง"
			prompt, err := Compile(brief, Options{VisualStyle: "anime", Mood: "warm", DesignFormat: tc.format}, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range []string{brief, "Anime", "Warm", tc.want, "supplied reference image", "Plain neutral background", "watermarks"} {
				if !strings.Contains(prompt, part) {
					t.Fatalf("missing %q", part)
				}
			}
		})
	}
}
func TestDefaultsAndInvalidOptions(t *testing.T) {
	t.Parallel()
	o, err := Normalize(Options{})
	if err != nil || o != (Options{VisualStyle: "auto", Mood: "neutral", DesignFormat: "full_body"}) {
		t.Fatalf("defaults %+v %v", o, err)
	}
	for _, o := range []Options{{VisualStyle: "bad"}, {Mood: "bad"}, {DesignFormat: "bad"}} {
		if _, err := Compile("brief", o, false); err == nil {
			t.Fatal("invalid options compiled")
		}
	}
}

func TestDescriptiveReferenceRetainsTraitsAndScopesUserEdits(t *testing.T) {
	t.Parallel()
	desc := "Face: oval, narrow jaw; Eyes and brows: wide brown eyes, straight brows; Nose and mouth: small nose, full lips; Hair: short silver curls; Build: lean; Clothing and accessories: red scarf; Palette: red and silver; Visual style: watercolor illustration."
	prompt, err := CompileDescription("ตัวละครชายหนุ่มสวมชุดนักโบราณคดี", Options{}, desc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{desc, "ตัวละครชายหนุ่มสวมชุดนักโบราณคดี", "Retain every described", "only to its corresponding traits", "not facial features or hairstyle", "Use the visual style and rendering treatment described", "Neutral, relaxed", "A-pose"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(prompt, "inspired only by these general") || strings.Contains(prompt, "The user's brief and selected style, mood and framing take priority.") {
		t.Fatal("legacy global override leaked")
	}
	explicit, err := CompileDescription("explorer", Options{VisualStyle: "anime", Mood: "warm", DesignFormat: "portrait"}, desc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Anime character design", "Warm, approachable", "head-and-shoulders", "in their own dimensions only", desc} {
		if !strings.Contains(explicit, want) {
			t.Fatalf("explicit missing %q", want)
		}
	}
	legacy, err := CompileLegacyDescription("explorer", Options{}, desc)
	if err != nil || !strings.Contains(legacy, "inspired only by these general visible traits") {
		t.Fatal("legacy prompt changed")
	}
	for _, bad := range []string{"", strings.Repeat("x", 4001)} {
		if _, err := CompileDescription("brief", Options{}, bad); err == nil {
			t.Fatal("invalid description accepted")
		}
	}
}

func TestSyntheticReferencePreservesStructureAndScopesChanges(t *testing.T) {
	t.Parallel()
	for _, reference := range []bool{false, true} {
		prompt, err := CompileSynthetic("archaeologist outfit", Options{VisualStyle: "anime", Mood: "warm", DesignFormat: "portrait"}, reference)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"synthetic, fictional character", "Do not identify a real person or promise an exact likeness", "archaeologist outfit", "Anime", "Warm", "head-and-shoulders"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("missing %q", want)
			}
		}
		if reference {
			for _, want := range []string{"supplied reference image", "face geometry", "hair silhouette", "body proportions", "palette, clothing and accessories", "only to their corresponding traits", "must not redesign the face or hair"} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("missing reference requirement %q", want)
				}
			}
		} else if strings.Contains(prompt, "supplied reference image") {
			t.Fatal("text-only job refers to absent reference")
		}
	}
	if _, err := CompileSynthetic("brief", Options{Mood: "invalid"}, true); err == nil {
		t.Fatal("invalid options accepted")
	}
}

func TestSyntheticAutoKeepsReferenceRenderingAndRealisticStyleUsesDigitalHuman(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"", "auto"} {
		prompt, err := CompileSynthetic("explorer", Options{VisualStyle: style}, true)
		if err != nil || !strings.Contains(prompt, "Preserve the supplied reference image's visual style") || !strings.Contains(prompt, "For a realistic human reference") || strings.Contains(prompt, styles["auto"]) {
			t.Fatalf("auto reference rendering lost: %v %s", err, prompt)
		}
	}
	prompt, err := CompileSynthetic("explorer", Options{VisualStyle: "cinematic_realistic"}, true)
	if err != nil || !strings.Contains(prompt, "lifelike digital-human rendering") {
		t.Fatalf("realistic synthetic rendering lost: %v %s", err, prompt)
	}
}
