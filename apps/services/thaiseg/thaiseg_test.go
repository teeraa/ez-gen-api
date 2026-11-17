package thaiseg

import (
	"strings"
	"testing"
)

func TestSegmentKnownSentences(t *testing.T) {
	cases := map[string]string{
		"ตอนนั้นนักวิทยาศาสตร์เชื่อว่า": "ตอนนั้น|นักวิทยาศาสตร์|เชื่อ|ว่า",
		"ปลาคืออยู่ในน้ำ":               "ปลา|คือ|อยู่|ใน|น้ำ",
		"หรือว่าน่าจะเป็น":              "หรือว่า|น่าจะเป็น",
		"ราคา 59 บาท":                   "ราคา| |59| |บาท",
	}
	for text, want := range cases {
		got := Segment(text)
		if strings.Join(got, "") != text {
			t.Fatalf("lossless: %q -> %q", text, got)
		}
		t.Logf("%s -> %s", text, strings.Join(got, "|"))
		if strings.Join(got, "|") != want {
			t.Errorf("%s: got %s want %s", text, strings.Join(got, "|"), want)
		}
	}
}

func TestWrapNeverCutsWords(t *testing.T) {
	text := "สิ่งมีชีวิตกลุ่มเนื้อในตอนนั้นนักวิทยาศาสตร์เชื่อว่าน่าจะเป็นอารมณ์แบบปลาคืออยู่ในน้ำ"
	lines := Wrap(text, 24)
	t.Logf("%q", lines)
	if strings.Join(lines, "") != text {
		t.Fatal("wrap must keep every character")
	}
	for _, l := range lines {
		if Width(l) > 24 {
			t.Fatalf("too wide %q", l)
		}
	}
	for _, bad := range []string{"ต\n", "\nอน", "ปล\n"} {
		if strings.Contains(strings.Join(lines, "\n"), bad) {
			t.Fatalf("word cut %q in %q", bad, lines)
		}
	}
	if got := Wrap("Tetrapod ทั้งหลาย", 24); len(got) != 1 || got[0] != "Tetrapod ทั้งหลาย" {
		t.Fatalf("mixed %q", got)
	}
}

func BenchmarkSegment(b *testing.B) {
	text := strings.Repeat("ตอนนั้นนักวิทยาศาสตร์เชื่อว่าปลาคืออยู่ในน้ำ", 5)
	for i := 0; i < b.N; i++ {
		Segment(text)
	}
}
