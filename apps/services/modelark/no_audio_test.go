package modelark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/volcengine/ark-runtime-go/arkruntime"
)

// Seedance makes audio unless generate_audio=false is sent, so NoAudio must reach the wire.
func TestNoAudioSendsGenerateAudioFalse(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  CreateTaskRequest
		want string
	}{
		{"silent", CreateTaskRequest{NoAudio: true}, "false"},
		{"audio", CreateTaskRequest{GenerateAudio: true}, "true"},
		{"default", CreateTaskRequest{}, ""},
	} {
		var got map[string]json.RawMessage
		transport := draftTransport(func(r *http.Request) (*http.Response, error) {
			if e := json.NewDecoder(r.Body).Decode(&got); e != nil {
				t.Fatal(e)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"task"}`))}, nil
		})
		client := &arkClient{sdk: newSDKClient("fixture-only", arkruntime.WithBaseUrl("https://fixture.invalid/api/v3"), arkruntime.WithHTTPClient(&http.Client{Transport: transport}), arkruntime.WithRetryTimes(0))}
		tc.req.Model = "dreamina-seedance-2-5-260628"
		tc.req.Content = []ContentItem{{Type: ContentTypeText, Text: "a shot"}}
		if _, e := client.CreateTask(context.Background(), tc.req); e != nil {
			t.Fatalf("%s: %v", tc.name, e)
		}
		if string(got["generate_audio"]) != tc.want {
			t.Fatalf("%s: generate_audio=%s, want %q", tc.name, got["generate_audio"], tc.want)
		}
	}
}
