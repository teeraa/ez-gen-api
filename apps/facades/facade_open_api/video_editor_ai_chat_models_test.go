package facade_open_api

import (
	"context"
	"errors"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
)

type modelTestCaller struct {
	name, model string
	ok          bool
}

func (c *modelTestCaller) Available() bool { return c.ok }
func (c *modelTestCaller) Name() string    { return c.name }
func (c *modelTestCaller) Model() string   { return c.model }
func (c *modelTestCaller) Chat(context.Context, editorai.ChatRequest) (editorai.ChatReply, error) {
	return editorai.ChatReply{}, nil
}

func TestAIChatModelDefaultAndPick(t *testing.T) {
	lite := &modelTestCaller{"gemini", "gemini-3.5-flash-lite", true}
	haiku := &modelTestCaller{"claude", "claude-haiku-5-5", true}
	sonnet := &modelTestCaller{"claude", "claude-sonnet-5-5", true}
	opus := &modelTestCaller{"claude", "claude-opus-5-5", false}
	callers := []editorai.ToolCaller{lite, haiku, sonnet, opus}

	f := NewVideoEditorAIChatFacade(nil, nil, callers, &modelTestCaller{"claude", "claude-sonnet-5-5", true}, true)
	if f.caller != sonnet {
		t.Fatalf("the configured provider is the default when listed: %v", editorai.ChatModelID(f.caller))
	}
	if c, err := f.pick(""); err != nil || c != sonnet {
		t.Fatal("empty id picks the default")
	}
	if c, err := f.pick("claude:claude-haiku-5-5"); err != nil || c != haiku {
		t.Fatal("listed model")
	}
	for _, id := range []string{"claude:claude-opus-5-5", "claude:claude-fable-5-1", "gemini:other"} {
		if _, err := f.pick(id); !errors.Is(err, ErrAIChatModel) {
			t.Fatalf("%s must be refused (unavailable or not offered)", id)
		}
	}
	models := f.models()
	if len(models) != 4 || models[1].Label != "Claude Haiku 5.5" || models[3].Available || models[0].InputPrice == nil || *models[0].InputPrice != 0.30 {
		t.Fatalf("models %+v", models)
	}

	unlisted := NewVideoEditorAIChatFacade(nil, nil, []editorai.ToolCaller{opus, haiku}, &modelTestCaller{"gemini", "x", true}, true)
	if unlisted.caller != haiku {
		t.Fatal("first available listed model when the configured one is not listed")
	}
	single := NewVideoEditorAIChatFacade(nil, nil, nil, lite, true)
	if len(single.callers) != 1 || single.caller != lite {
		t.Fatal("no list: the configured provider is the only choice")
	}
}
