package facade_open_api

import "testing"

func TestFilmTrimRoundsAndKeepsOneSecond(t *testing.T) {
	in, out, ok := filmTrim(0.44, 1.26, 6)
	if !ok || in != 0.4 || out != 1.3 {
		t.Fatalf("trims round to 0.1 s: %v %v %v", in, out, ok)
	}
	if _, _, ok := filmTrim(3, 2.5, 6); ok {
		t.Fatal("less than a second left must be rejected")
	}
	if _, _, ok := filmTrim(-1, 0, 6); ok {
		t.Fatal("negative trim must be rejected")
	}
}

func TestFilmClipWindow(t *testing.T) {
	if s, e := filmClipWindow(filmClipTrim{}, 5.04); s != 0 || e != 0 {
		t.Fatalf("no trim keeps the whole clip: %v %v", s, e)
	}
	if s, e := filmClipWindow(filmClipTrim{In: 1}, 5.04); s != 1 || e != 0 {
		t.Fatalf("start trim only: %v %v", s, e)
	}
	if s, e := filmClipWindow(filmClipTrim{In: 1, Out: 1.5}, 5.04); s != 1 || e < 3.53 || e > 3.55 {
		t.Fatalf("both trims measure from the real clip length: %v %v", s, e)
	}
}

func TestFilmExportSourceKeepsOldHashWithoutStudioEdits(t *testing.T) {
	d := filmDocument{Shots: []filmShot{{ID: "a", Video: &filmVideo{Status: "succeeded", ClipKey: "k1"}}, {ID: "b"}}}
	plain := filmExportSourceOf(&d)
	if plain != filmExportSource([]string{"k1"}) {
		t.Fatal("an episode without trims or music keeps the hash older exports stored")
	}
	d.Shots[0].TrimIn = 0.5
	trimmed := filmExportSourceOf(&d)
	if trimmed == plain {
		t.Fatal("a trim makes the export stale")
	}
	d.Music = &filmMusic{Key: "m", Volume: 0.3}
	if filmExportSourceOf(&d) == trimmed {
		t.Fatal("music makes the export stale")
	}
}

func TestFilmAudioKind(t *testing.T) {
	mp3 := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, 32)...)
	if ext, _ := filmAudioKind(mp3); ext != ".mp3" {
		t.Fatalf("mp3: %q", ext)
	}
	m4a := append([]byte{0, 0, 0, 0x20}, []byte("ftypM4A ")...)
	if ext, kind := filmAudioKind(m4a); ext != ".m4a" || kind != "audio/mp4" {
		t.Fatalf("m4a: %q %q", ext, kind)
	}
	if ext, _ := filmAudioKind([]byte("<html>not audio</html>")); ext != "" {
		t.Fatal("non-audio is rejected")
	}
}

func TestFilmLineAtAndPlaces(t *testing.T) {
	if at, ok := filmLineAt([]float64{0.44, 3}, 6); !ok || at[0] != 0.4 || at[1] != 3 {
		t.Fatalf("line places round to 0.1: %v %v", at, ok)
	}
	if _, ok := filmLineAt([]float64{7}, 6); ok {
		t.Fatal("a line cannot start after the clip")
	}
	sh := &filmShot{Seconds: 6, Video: &filmVideo{Status: "succeeded", ClipKey: "c", RawKey: "r", Lines: []filmDubLine{{Key: "l0", At: 0.4, Seconds: 1}, {Key: "l1", At: 1.6, Seconds: 1}}}}
	if p := filmLinePlaces(sh); p[0].At != 0.4 || p[1].At != 1.6 {
		t.Fatalf("generated places without moves: %+v", p)
	}
	d := filmDocument{Shots: []filmShot{*sh}}
	before := filmExportSourceOf(&d)
	if dubs := filmExportDubs(&d); len(dubs) != 1 || dubs[0] != nil {
		t.Fatal("an unmoved clip is exported as it is")
	}
	d.Shots[0].LineAt = []float64{2, 0.5}
	if p := filmLinePlaces(&d.Shots[0]); p[0].At != 2 || p[1].At != 0.5 {
		t.Fatalf("moved places: %+v", p)
	}
	if dubs := filmExportDubs(&d); dubs[0] == nil || dubs[0].RawKey != "r" || dubs[0].Lines[0].At != 2 {
		t.Fatal("a clip with moved lines is mixed again from its raw clip")
	}
	if filmExportSourceOf(&d) == before {
		t.Fatal("moving a line makes the export stale")
	}
}
