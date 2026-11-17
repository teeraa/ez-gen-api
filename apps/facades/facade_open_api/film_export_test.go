package facade_open_api

import "testing"

func TestFilmExportClips(t *testing.T) {
	d := filmDocument{Shots: []filmShot{
		{ID: "a", Dialogue: []filmLine{{Line: "ไปกันเถอะ"}, {Line: "เร็วเข้า"}}, Video: &filmVideo{Status: "succeeded", ClipKey: "k1"}},
		{ID: "b", Video: &filmVideo{Status: "processing"}},
		{ID: "c"},
		{ID: "d", Video: &filmVideo{Status: "succeeded", ClipKey: "k2"}},
	}}
	keys, subs, skipped := filmExportClips(&d, nil)
	if len(keys) != 2 || keys[0] != "k1" || keys[1] != "k2" || skipped != 2 {
		t.Fatalf("keys=%v skipped=%d", keys, skipped)
	}
	if subs[0] != "ไปกันเถอะ เร็วเข้า" || subs[1] != "" {
		t.Fatalf("subs=%q", subs)
	}
	if filmExportSource(keys) == filmExportSource([]string{"k2", "k1"}) {
		t.Fatal("source must depend on clip order")
	}
}

func TestFilmTextLayersRange(t *testing.T) {
	subs := []string{"a", "", "c", "d"}
	all := filmTextLayers(subs, 0)
	if len(all) != 3 || all[1].ClipIdx != 2 || all[1].Position != "bottom" {
		t.Fatalf("all=%+v", all)
	}
	part := filmTextLayersRange(subs, 2)
	if len(part) != 2 || part[0].ClipIdx != 0 || part[0].Text != "c" || part[1].ClipIdx != 1 {
		t.Fatalf("part=%+v", part)
	}
	if filmTextLayersRange(subs, 4) != nil {
		t.Fatal("empty range must be nil")
	}
}
