package characterdesign

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestPipelineCharacterDesignIsVisiblySynthetic(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"", "auto", "cinematic_realistic", "3d_animation", "anime", "clay", "illustration"} {
		prompt, err := CompilePipeline("medieval man in a suit", Options{VisualStyle: style}, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"clearly synthetic, fictional", "Redesign facial geometry", "single full-body", "medieval man in a suit"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s missing %s", style, want)
			}
		}
		for _, forbidden := range []string{"natural skin", "Retain its face geometry", "realistic skin and fabric"} {
			if strings.Contains(prompt, forbidden) {
				t.Errorf("%s contains conflicting %s", style, forbidden)
			}
		}
		if style == "cinematic_realistic" && !strings.Contains(prompt, "Clearly stylized CG") {
			t.Fatal("realistic option must use synthetic intermediate")
		}
		if style == "anime" && !strings.Contains(prompt, "Anime character design") {
			t.Fatal("explicit anime style lost")
		}
	}
}

func TestPipelineDigitalHumanKeepsNaturalReferenceStructure(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"", "auto", "cinematic_realistic"} {
		for _, reference := range []bool{false, true} {
			prompt, err := CompilePipelineRealistic("man in a suit", Options{VisualStyle: style}, reference)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"fictional digital-human", "natural human anatomy", "subtle synthetic rendering treatment", "single full-body", "man in a suit"} {
				if !strings.Contains(prompt, want) {
					t.Errorf("missing %q", want)
				}
			}
			if reference && !strings.Contains(prompt, "Retain a similar overall facial structure") {
				t.Fatal("reference structure instruction missing")
			}
			for _, forbidden := range []string{"Clearly stylized CG", "Redesign facial geometry", "for an animated production"} {
				if strings.Contains(prompt, forbidden) {
					t.Errorf("v1 instruction persists: %s", forbidden)
				}
			}
		}
	}
	if _, err := CompilePipelineRealistic("brief", Options{VisualStyle: "invalid"}, true); err == nil {
		t.Fatal("invalid style accepted")
	}
}

func TestPipelineSoftDigitalHumanOnlySoftensSurfaceRendering(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"", "auto", "cinematic_realistic"} {
		for _, reference := range []bool{false, true} {
			prompt, err := CompilePipelineSoftRealistic("man in a suit", Options{VisualStyle: style}, reference)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Natural human facial and body proportions", "subtly softened, gently smoothed skin", "lightly illustrated digital finish", "realistic hair, clothing", "Omit fine photographic skin pores", "single full-body"} {
				if !strings.Contains(prompt, want) {
					t.Errorf("missing %q", want)
				}
			}
			if reference && !strings.Contains(prompt, "Soften only the surface rendering slightly; do not redesign the face") {
				t.Fatal("reference proportions not protected")
			}
			if strings.Contains(prompt, "for a photorealistic cinematic production") || strings.Contains(prompt, "Redesign facial geometry") {
				t.Fatal("conflicting image treatment")
			}
		}
	}
	if _, err := CompilePipelineSoftRealistic("brief", Options{VisualStyle: "invalid"}, true); err == nil {
		t.Fatal("invalid style accepted")
	}
}

func TestPipelineV2ImagePromptRemainsByteStable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		reference bool
		want      string
	}{
		{false, "ecb426d1e9da970a06c0a8905bd4876e5ebfa3848c4ec47a580054463645597a"},
		{true, "caa5d1a026e9e73fa242091fd1c5788496823b7a9d3b062c9d65fe2f9345edcf"},
	} {
		prompt, err := CompilePipelineRealistic("man in a suit", Options{VisualStyle: "cinematic_realistic"}, tc.reference)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(prompt))); got != tc.want {
			t.Fatalf("frozen v2 prompt changed: %s", got)
		}
	}
}

func TestPipelineSemiRealisticCharacterMakesSyntheticRenderingClear(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"", "auto", "cinematic_realistic"} {
		for _, reference := range []bool{false, true} {
			prompt, err := CompilePipelineSemiRealistic("man in a suit", Options{VisualStyle: style}, reference)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Clearly authored semi-realistic digital character", "smooth stylized skin", "slightly sculpted clean facial planes", "simplified fine surface texture", "natural human facial and body proportions", "not a photo portrait", "No extreme cartoon, chibi, toy-like", "single full-body"} {
				if !strings.Contains(prompt, want) {
					t.Errorf("missing %q", want)
				}
			}
			if reference && !strings.Contains(prompt, "Preserve recognizable broad facial structure") {
				t.Fatal("reference structure instruction missing")
			}
			for _, forbidden := range []string{"Soften only the surface rendering slightly", "Redesign facial geometry", "for a photorealistic cinematic production"} {
				if strings.Contains(prompt, forbidden) {
					t.Errorf("conflicting image treatment %q", forbidden)
				}
			}
		}
	}
	if _, err := CompilePipelineSemiRealistic("brief", Options{VisualStyle: "invalid"}, true); err == nil {
		t.Fatal("invalid style accepted")
	}
}

func TestPipelineV3ImagePromptRemainsByteStable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		reference bool
		want      string
	}{
		{false, "93949e3ec3c9eb427ac6c5182aea8b9a548028bfae547492355091c5cf2d7f53"},
		{true, "6c8872ed5937845299b9f6455ac51e4af709bde1aef4963f46be97a0288c38dd"},
	} {
		prompt, err := CompilePipelineSoftRealistic("man in a suit", Options{VisualStyle: "cinematic_realistic"}, tc.reference)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(prompt))); got != tc.want {
			t.Fatalf("frozen v3 prompt changed: %s", got)
		}
	}
}

func TestPipelinePolished3DPreservesStructureAndSelectedMood(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"", "auto", "cinematic_realistic"} {
		for _, mood := range []string{"neutral", "warm", "confident"} {
			for _, reference := range []bool{false, true} {
				brief := "an explorer wearing a linen jacket"
				prompt, err := CompilePipelinePolished3D(brief, Options{VisualStyle: style, Mood: mood}, reference)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{brief, "Polished semi-realistic 3D digital character", "natural human anatomy", "softly smoothed skin", "subtle tonal variation and continuous gentle facial shading", "without redesigning facial planes", "defined softly shaded locks and finer strands", "realistic fabric texture, seams, folds and thickness", "Art direction (apply independently from the clothing brief)", "Plain light-gray studio background", "soft diffuse studio lighting", "single full-body", moods[mood]} {
					if !strings.Contains(prompt, want) {
						t.Errorf("missing %q", want)
					}
				}
				if mood != "neutral" && strings.Contains(prompt, moods["neutral"]) {
					t.Fatal("selected expression contradicted by neutral default")
				}
				if reference {
					for _, want := range []string{"relative eye size and spacing, eyebrows, nose, mouth, jaw, hair silhouette", "generic idealized face", "Changing clothing or accessories must not replace the face or hair"} {
						if !strings.Contains(prompt, want) {
							t.Errorf("reference instruction missing %q", want)
						}
					}
				}
				for _, forbidden := range []string{"slightly sculpted clean facial planes", "Redesign facial geometry", "man in a suit", "adult human", "for a photorealistic cinematic production"} {
					if strings.Contains(prompt, forbidden) {
						t.Errorf("unrequested instruction %q", forbidden)
					}
				}
			}
		}
	}
	if _, err := CompilePipelinePolished3D("brief", Options{VisualStyle: "invalid"}, true); err == nil {
		t.Fatal("invalid style accepted")
	}
}

func TestPipelineV4ImagePromptRemainsByteStable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		reference bool
		want      string
	}{
		{false, "1a1af48bffc9a6e555984c0ede9085a69e13646dedcf4d406146cd40367733d9"},
		{true, "1159837ad9025db8889921b9521a9fa18f45e4dd7003a1678401898a0d9f7357"},
	} {
		prompt, err := CompilePipelineSemiRealistic("man in a suit", Options{VisualStyle: "cinematic_realistic"}, tc.reference)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(prompt))); got != tc.want {
			t.Fatalf("frozen v4 prompt changed: %s", got)
		}
	}
}

func TestPipelineStylized3DUsesMatteRenderedSurface(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"", "auto", "cinematic_realistic"} {
		for _, mood := range []string{"neutral", "warm", "confident"} {
			for _, reference := range []bool{false, true} {
				brief := "an explorer wearing a linen jacket"
				prompt, err := CompilePipelineStylized3D(brief, Options{VisualStyle: style, Mood: mood}, reference)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{brief, "Visibly stylized 3D fictional character", "clearly rendered digital surface", "smooth matte skin and simplified microtexture", "slightly stylized facial forms", "natural human facial and body proportions", "detailed stylized locks", "simplified fine weave", "not a photographic portrait or a retouched photo", moods[mood], "single full-body"} {
					if !strings.Contains(prompt, want) {
						t.Errorf("missing %q", want)
					}
				}
				if mood != "neutral" && strings.Contains(prompt, moods["neutral"]) {
					t.Fatal("selected mood contradicted by neutral default")
				}
				if reference && !strings.Contains(prompt, "Preserve broad face shape") {
					t.Fatal("broad reference resemblance missing")
				}
				for _, forbidden := range []string{"Polished semi-realistic 3D digital character", "defined softly shaded locks and finer strands", "man in a suit", "for a photorealistic cinematic production"} {
					if strings.Contains(prompt, forbidden) {
						t.Errorf("conflicting or unrequested instruction %q", forbidden)
					}
				}
			}
		}
	}
	if _, err := CompilePipelineStylized3D("brief", Options{VisualStyle: "invalid"}, true); err == nil {
		t.Fatal("invalid style accepted")
	}
}

func TestPipelineV5ImagePromptRemainsByteStable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		reference bool
		want      string
	}{
		{false, "d6ec02860c8b51597a4f61e4ee19f36491fcb2a416c4938faad9a2d873385aeb"},
		{true, "619827680e9076f0e25b80c194eb30339912ca3448eeaf7e65da58280dd29f97"},
	} {
		prompt, err := CompilePipelinePolished3D("man in a suit", Options{VisualStyle: "cinematic_realistic"}, tc.reference)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(prompt))); got != tc.want {
			t.Fatalf("frozen v5 prompt changed: %s", got)
		}
	}
}
