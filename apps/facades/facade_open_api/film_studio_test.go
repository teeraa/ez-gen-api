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
