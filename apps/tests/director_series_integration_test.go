package tests

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/google/uuid"
)

func (d *directorFixture) data(method, path string, body any, want int, out any) {
	d.t.Helper()
	status, b, _ := d.http(method, path, body, nil)
	if status != want {
		d.t.Fatalf("%s %s: want %d got %d %s", method, path, want, status, b)
	}
	if out == nil {
		return
	}
	var env struct {
		Data      json.RawMessage `json:"payload"`
		RequestID string          `json:"transactionCode"`
	}
	if err := json.Unmarshal(b, &env); err != nil || env.RequestID == "" {
		d.t.Fatalf("envelope: %v %s", err, b)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		d.t.Fatal(err)
	}
}

func TestDirectorPostgresSeriesEpisodes(t *testing.T) {
	d := newDirectorFixture(t)
	if err := d.db.Exec(`CREATE TABLE library_assets(id uuid PRIMARY KEY,user_id uuid NOT NULL,name text,asset_type text NOT NULL,description text,prompt text,storage_key text,deleted_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	character := uuid.NewString()
	if err := d.db.Exec(`INSERT INTO library_assets(id,user_id,name,asset_type,description,storage_key) VALUES (?,?,'ดาว','character','เด็กหญิงผมสั้น','characters/dao.png')`, character, d.user).Error; err != nil {
		t.Fatal(err)
	}
	stranger := map[string]string{"X-Test-User": uuid.NewString()}

	key := uuid.NewString()
	body := map[string]any{"name": "ผจญภัยดวงดาว", "aspectRatio": "9:16", "style": "anime", "resolution": "720p", "characterAssetIds": []string{character}, "idempotencyKey": key}
	var series dto.DirectorSeries
	d.data("POST", "/series", body, 201, &series)
	if series.Revision != 1 || series.EpisodeCount != 0 || series.CoverImageURL != nil || len(series.CharacterAssetIDs) != 1 {
		t.Fatalf("series: %+v", series)
	}
	var replay dto.DirectorSeries
	d.data("POST", "/series", body, 201, &replay)
	if replay.ID != series.ID {
		t.Fatal("idempotent replay created a second series")
	}
	body["name"] = "อื่น"
	d.wantError("POST", "/series", body, 409, "IDEMPOTENCY_CONFLICT")
	d.wantError("POST", "/series", map[string]any{"name": strings.Repeat("ก", 121), "aspectRatio": "9:16", "style": "anime", "resolution": "720p", "idempotencyKey": uuid.NewString()}, 400, "VALIDATION_ERROR")
	d.wantError("POST", "/series", map[string]any{"name": "x", "aspectRatio": "9:16", "style": "anime", "resolution": "720p", "idempotencyKey": uuid.NewString(), "characterAssetIds": []string{uuid.NewString()}}, 400, "VALIDATION_ERROR")
	d.wantError("POST", "/series", map[string]any{"name": "x", "aspectRatio": "9:16", "style": "anime", "resolution": "720p", "idempotencyKey": uuid.NewString(), "bogus": 1}, 400, "VALIDATION_ERROR")

	base := "/series/" + series.ID
	epKey := uuid.NewString()
	first := d.project("POST", base+"/episodes", map[string]any{"idempotencyKey": epKey})
	again := d.project("POST", base+"/episodes", map[string]any{"idempotencyKey": epKey})
	second := d.project("POST", base+"/episodes", map[string]any{"title": "เมืองใต้ทะเล", "idempotencyKey": uuid.NewString()})
	if again.ID != first.ID {
		t.Fatal("episode replay created a duplicate")
	}
	if first.SeriesID == nil || *first.SeriesID != series.ID || *first.EpisodeNumber != 1 || first.EpisodeTitle != nil || first.Name != "ผจญภัยดวงดาว ตอนที่ 1" {
		t.Fatalf("episode 1: %+v %v %v %q", first.SeriesID, first.EpisodeNumber, first.EpisodeTitle, first.Name)
	}
	if *second.EpisodeNumber != 2 || second.EpisodeTitle == nil || *second.EpisodeTitle != "เมืองใต้ทะเล" || second.Name != "เมืองใต้ทะเล" {
		t.Fatalf("episode 2: %v %v %q", second.EpisodeNumber, second.EpisodeTitle, second.Name)
	}
	if first.Step != "idea" || first.AspectRatio != "9:16" || first.Style != "anime" || first.Resolution != "720p" || len(first.Idea.CharacterAssetIDs) != 1 || first.Idea.CharacterAssetIDs[0] != character {
		t.Fatalf("defaults not copied: %+v", first)
	}
	d.wantError("POST", base+"/episodes", map[string]any{"title": strings.Repeat("ต", 121), "idempotencyKey": uuid.NewString()}, 400, "VALIDATION_ERROR")

	// Concurrent creates must still get distinct, contiguous numbers.
	var wg sync.WaitGroup
	numbers := make(chan int, 6)
	errs := make(chan string, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, b, _ := d.http("POST", base+"/episodes", map[string]any{"idempotencyKey": uuid.NewString()}, nil)
			var out struct {
				Data dto.DirectorProject `json:"payload"`
			}
			if status != 201 || json.Unmarshal(b, &out) != nil || out.Data.EpisodeNumber == nil {
				errs <- fmt.Sprintf("%d %s", status, b)
				return
			}
			numbers <- *out.Data.EpisodeNumber
		}()
	}
	wg.Wait()
	close(numbers)
	close(errs)
	for e := range errs {
		t.Fatalf("concurrent episode create: %s", e)
	}
	seen := map[int]bool{}
	for n := range numbers {
		if seen[n] || n < 3 || n > 8 {
			t.Fatalf("duplicate or unexpected episode number %d", n)
		}
		seen[n] = true
	}

	var detail dto.DirectorSeriesDetail
	d.data("GET", base, nil, 200, &detail)
	if detail.EpisodeCount != 8 || len(detail.Episodes) != 8 {
		t.Fatalf("detail count: %d %d", detail.EpisodeCount, len(detail.Episodes))
	}
	for i, ep := range detail.Episodes {
		if ep.EpisodeNumber == nil || *ep.EpisodeNumber != i+1 || ep.SeriesID == nil || *ep.SeriesID != series.ID {
			t.Fatalf("detail order at %d: %+v", i, ep)
		}
	}
	// Delete episodes 3..8 through the existing project endpoint to keep the rest of the test small.
	for _, ep := range detail.Episodes[2:] {
		if status, b, _ := d.http("DELETE", "/projects/"+ep.ID, nil, nil); status != 204 {
			t.Fatalf("episode delete %d %s", status, b)
		}
	}

	standalone := d.create()
	var list dto.DirectorList
	d.data("GET", "/projects", nil, 200, &list)
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != standalone.ID || list.Items[0].SeriesID != nil || list.Items[0].EpisodeNumber != nil {
		t.Fatalf("legacy list must contain only standalone projects: %+v", list)
	}
	raw := map[string]any{}
	status, b, _ := d.http("GET", "/projects/"+standalone.ID, nil, nil)
	if status != 200 || json.Unmarshal(b, &struct {
		Data *map[string]any `json:"payload"`
	}{&raw}) != nil {
		t.Fatalf("standalone get %d %s", status, b)
	}
	for _, field := range []string{"seriesId", "episodeNumber", "episodeTitle"} {
		if v, ok := raw[field]; !ok || v != nil {
			t.Fatalf("standalone %s must be present and null: %v", field, raw)
		}
	}
	var seriesList dto.DirectorSeriesList
	d.data("GET", "/series?page=1&limit=20", nil, 200, &seriesList)
	if seriesList.Total != 1 || len(seriesList.Items) != 1 || seriesList.Items[0].EpisodeCount != 2 || seriesList.Limit != 20 {
		t.Fatalf("series list: %+v", seriesList)
	}
	d.wantError("GET", "/series?limit=51", nil, 400, "VALIDATION_ERROR")

	for _, probe := range []struct {
		method, path string
		body         any
	}{{"GET", base, nil}, {"PATCH", base, map[string]any{"revision": 1, "name": "x"}}, {"DELETE", base, nil}, {"POST", base + "/episodes", map[string]any{"idempotencyKey": uuid.NewString()}}, {"GET", "/projects/" + first.ID, nil}} {
		if status, b, _ := d.http(probe.method, probe.path, probe.body, stranger); status != 404 || !strings.Contains(string(b), "PROJECT_NOT_FOUND") {
			t.Fatalf("tenant %s %s: %d %s", probe.method, probe.path, status, b)
		}
	}
	status, b, _ = d.http("GET", "/series", nil, stranger)
	if status != 200 || !strings.Contains(string(b), `"total":0`) {
		t.Fatalf("stranger list: %d %s", status, b)
	}

	d.wantError("PATCH", base, map[string]any{"revision": 0, "name": "ใหม่"}, 409, "REVISION_CONFLICT")
	var patched dto.DirectorSeries
	d.data("PATCH", base, map[string]any{"revision": 1, "name": "ดวงดาวภาคสอง", "style": "watercolor"}, 200, &patched)
	if patched.Revision != 2 || patched.Name != "ดวงดาวภาคสอง" || patched.Style != "watercolor" {
		t.Fatalf("patch: %+v", patched)
	}
	var noop dto.DirectorSeries
	d.data("PATCH", base, map[string]any{"revision": 2}, 200, &noop)
	if noop.Revision != 2 || !noop.UpdatedAt.Equal(patched.UpdatedAt) {
		t.Fatalf("no-op patch changed revision/updatedAt: %+v vs %+v", noop, patched)
	}
	if ep := d.get(first.ID); ep.Style != "anime" || ep.Name != "ผจญภัยดวงดาว ตอนที่ 1" {
		t.Fatal("changing series defaults must not rewrite existing episodes")
	}
	renamed := d.project("PATCH", "/projects/"+first.ID, map[string]any{"revision": first.Revision, "episodeTitle": "จุดเริ่มต้น"})
	if renamed.EpisodeTitle == nil || *renamed.EpisodeTitle != "จุดเริ่มต้น" || renamed.Revision != first.Revision+1 {
		t.Fatalf("episode title patch: %+v", renamed.EpisodeTitle)
	}
	d.wantError("PATCH", "/projects/"+standalone.ID, map[string]any{"revision": standalone.Revision, "episodeTitle": "x"}, 400, "VALIDATION_ERROR")

	// The existing pipeline runs on an episode; episode 2's story sees episode 1 as context.
	d.fund(100)
	prompts := []string{}
	var mu sync.Mutex
	d.analyzer.reply = func(prompt string) (string, error) {
		mu.Lock()
		prompts = append(prompts, prompt)
		mu.Unlock()
		return directorPlanner(prompt)
	}
	ep1 := d.project("PUT", "/projects/"+first.ID+"/idea", map[string]any{"revision": renamed.Revision, "prompt": "เด็กน้อยออกเดินทางตามหาดวงดาวที่หายไป", "genre": "adventure", "moods": []string{"อบอุ่น"}, "sceneCount": 1, "characterAssetIds": []string{}, "storyModelId": "director-story"})
	ep1 = d.action(ep1, "/story")
	d.tick()
	if ep1 = d.get(ep1.ID); ep1.Story.Status != "succeeded" || ep1.Story.Logline == nil {
		t.Fatalf("episode story: %+v", ep1.Story)
	}
	ep2 := d.idea(d.get(second.ID))
	ep2 = d.action(ep2, "/story")
	d.tick()
	if ep2 = d.get(ep2.ID); ep2.Story.Status != "succeeded" {
		t.Fatalf("episode 2 story: %+v", ep2.Story)
	}
	last := prompts[len(prompts)-1]
	marker := "Project and instruction data: "
	var planner struct {
		Series struct {
			Name            string `json:"name"`
			EpisodeNumber   int    `json:"episodeNumber"`
			EpisodeTitle    string `json:"episodeTitle"`
			EarlierEpisodes []struct {
				Number  int    `json:"number"`
				Title   string `json:"title"`
				Logline string `json:"logline"`
			} `json:"earlierEpisodes"`
		} `json:"series"`
	}
	if err := json.Unmarshal([]byte(last[strings.LastIndex(last, marker)+len(marker):]), &planner); err != nil {
		t.Fatal(err)
	}
	s := planner.Series
	if s.Name != "ดวงดาวภาคสอง" || s.EpisodeNumber != 2 || s.EpisodeTitle != "เมืองใต้ทะเล" || len(s.EarlierEpisodes) != 1 || s.EarlierEpisodes[0].Number != 1 || s.EarlierEpisodes[0].Title != "จุดเริ่มต้น" || s.EarlierEpisodes[0].Logline != *ep1.Story.Logline {
		t.Fatalf("episode 2 series context: %+v", s)
	}
	ep2 = d.project("PATCH", "/projects/"+ep2.ID+"/scenes/"+ep2.Scenes[0].ID, map[string]any{"revision": ep2.Revision, "shotCount": 1})
	ep2 = d.action(ep2, "/storyboard")
	for i := 0; i < 6 && ep2.Storyboard.Status != "succeeded"; i++ {
		d.tick()
		ep2 = d.get(ep2.ID)
	}
	if ep2.Storyboard.Status != "succeeded" || ep2.CoverImageURL == nil {
		t.Fatalf("episode storyboard: %+v", ep2.Storyboard)
	}
	d.data("GET", base, nil, 200, &detail)
	if detail.CoverImageURL == nil || len(detail.Episodes) != 2 || detail.Episodes[1].StoryStatus != "succeeded" || detail.Episodes[0].CoverImageURL != nil || detail.Episodes[1].CoverImageURL == nil {
		t.Fatalf("series cover/summary: %+v", detail.DirectorSeries)
	}
	shotCover := *detail.CoverImageURL
	if shotCover != *ep2.CoverImageURL {
		t.Fatalf("detail cover %s want episode 2 cover %s", shotCover, *ep2.CoverImageURL)
	}
	d.data("GET", "/series", nil, 200, &seriesList)
	if len(seriesList.Items) != 1 || seriesList.Items[0].EpisodeCount != 2 || seriesList.Items[0].CoverImageURL == nil || *seriesList.Items[0].CoverImageURL != shotCover {
		t.Fatalf("SQL list cover must match the first shot of episode 2: %+v", seriesList.Items)
	}

	// A queued job on any episode blocks series deletion; nothing is deleted.
	queued := d.action(d.get(first.ID), "/cover")
	if queued.Cover == nil || queued.Cover.Status != "queued" {
		t.Fatal("cover job not queued")
	}
	d.wantError("DELETE", base, nil, 409, "SERIES_BUSY")
	d.get(first.ID)
	d.data("GET", base, nil, 200, &detail)
	d.tick()
	covered := d.get(first.ID)
	if covered.Cover == nil || covered.Cover.Status != "succeeded" || covered.CoverImageURL == nil || *covered.CoverImageURL == shotCover {
		t.Fatalf("cover job did not finish: %+v", covered.Cover)
	}
	d.data("GET", base, nil, 200, &detail)
	d.data("GET", "/series", nil, 200, &seriesList)
	if detail.CoverImageURL == nil || *detail.CoverImageURL != *covered.CoverImageURL || seriesList.Items[0].CoverImageURL == nil || *seriesList.Items[0].CoverImageURL != *covered.CoverImageURL {
		t.Fatal("series cover must come from the lowest-numbered episode with a cover, in detail and SQL list alike")
	}
	if status, b, _ := d.http("DELETE", base, nil, nil); status != 204 {
		t.Fatalf("series delete %d %s", status, b)
	}
	d.wantError("GET", base, nil, 404, "PROJECT_NOT_FOUND")
	d.wantError("GET", "/projects/"+first.ID, nil, 404, "PROJECT_NOT_FOUND")
	d.wantError("GET", "/projects/"+second.ID, nil, 404, "PROJECT_NOT_FOUND")
	d.wantError("POST", base+"/episodes", map[string]any{"idempotencyKey": epKey}, 404, "PROJECT_NOT_FOUND")
	d.wantError("POST", "/series", map[string]any{"name": "ผจญภัยดวงดาว", "aspectRatio": "9:16", "style": "anime", "resolution": "720p", "characterAssetIds": []string{character}, "idempotencyKey": key}, 404, "PROJECT_NOT_FOUND")
	d.data("GET", "/series", nil, 200, &seriesList)
	if seriesList.Total != 0 {
		t.Fatalf("deleted series still listed: %+v", seriesList)
	}
	if p := d.get(standalone.ID); p.ID != standalone.ID {
		t.Fatal("standalone project affected by series delete")
	}
}
