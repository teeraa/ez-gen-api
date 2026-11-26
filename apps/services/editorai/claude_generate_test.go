package editorai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClaudeGenerateForcesSchemaTool(t *testing.T) {
	for _, tc := range []struct {
		name, schema, reply, want string
	}{
		{"object schema", `{"type":"object","properties":{"a":{"type":"integer"}}}`, `{"a":1}`, `{"a":1}`},
		{"array schema is wrapped", `{"type":"array","items":{"type":"string"}}`, `{"result":["x","y"]}`, `["x","y"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				var in struct {
					ToolChoice struct{ Type, Name string } `json:"tool_choice"`
					Tools      []struct {
						InputSchema struct{ Type string } `json:"input_schema"`
					} `json:"tools"`
				}
				if json.Unmarshal(raw, &in) != nil || in.ToolChoice.Name != "respond" || len(in.Tools) != 1 || in.Tools[0].InputSchema.Type != "object" {
					w.WriteHeader(418)
					return
				}
				_, _ = w.Write([]byte(`{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"t1","name":"respond","input":` + tc.reply + `}]}`))
			}))
			defer srv.Close()
			got, err := NewClaudeWithEndpoint("k", "m", srv.URL, srv.Client()).Generate(context.Background(), "SYS", "PROMPT", []Image{{AssetID: "a", Data: []byte{1}}}, json.RawMessage(tc.schema))
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %s %v", got, err)
			}
		})
	}
}
