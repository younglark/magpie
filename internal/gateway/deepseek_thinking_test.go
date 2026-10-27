package gateway

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// OpenCode deepseekChat (packages/core/src/variant.ts): effort none → only
// thinking:{type:"disabled"} (no reasoning_effort); otherwise
// reasoning_effort + thinking:{type:"enabled"}. Magpie's Chat rebuild matches
// that for DeepSeek-family hosts/models, and leaves other Chat upstreams
// without the thinking switch.
func TestBuildChatDeepSeekThinkingType(t *testing.T) {
	msg := []Message{{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}}}
	for _, c := range []struct {
		name, host, model string
		req               Request
		wantEffort        string // "" = absent
		wantThinking      string // "", "enabled", "disabled"
		forbidThinking    bool
	}{
		{
			name: "deepseek host high", host: "api.deepseek.com", model: "deepseek-chat",
			req:        Request{Thinking: true, Effort: "high"},
			wantEffort: "high", wantThinking: "enabled",
		},
		{
			name: "deepseek host none", host: "api.deepseek.com", model: "deepseek-chat",
			req:        Request{ThinkOff: true, Effort: "none"},
			wantEffort: "", wantThinking: "disabled",
		},
		{
			name: "deepseek ThinkOff alone", host: "api.deepseek.com", model: "deepseek-reasoner",
			req:        Request{ThinkOff: true},
			wantEffort: "", wantThinking: "disabled",
		},
		{
			name: "opencode-go flash medium", host: "opencode.ai", model: "deepseek-v4.1-flash",
			req:        Request{Thinking: true, Effort: "medium"},
			wantEffort: "medium", wantThinking: "enabled",
		},
		{
			name: "opencode-go flash off", host: "opencode.ai", model: "deepseek-v4.1-flash",
			req:        Request{ThinkOff: true},
			wantEffort: "", wantThinking: "disabled",
		},
		{
			name: "thinking toggle only", host: "api.deepseek.com", model: "deepseek-chat",
			req:        Request{Thinking: true},
			wantEffort: "", wantThinking: "enabled",
		},
		{
			name: "openai no thinking field", host: "api.openai.com", model: "gpt-5",
			req:        Request{Thinking: true, Effort: "high"},
			wantEffort: "high", forbidThinking: true,
		},
		{
			name: "kimi no deepseek thinking", host: "api.moonshot.cn", model: "kimi-k2",
			req:        Request{Thinking: true, Effort: "high"},
			wantEffort: "high", forbidThinking: true,
		},
		{
			name: "no effort no thinking", host: "api.deepseek.com", model: "deepseek-chat",
			req:        Request{},
			wantEffort: "", wantThinking: "",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := c.req
			r.Messages = msg
			raw := buildChat(&r, c.model, c.host, false)
			var out struct {
				Effort   string `json:"reasoning_effort"`
				Thinking *struct {
					Type string `json:"type"`
				} `json:"thinking"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			if out.Effort != c.wantEffort {
				t.Errorf("reasoning_effort = %q, want %q; body %s", out.Effort, c.wantEffort, raw)
			}
			gotType := ""
			if out.Thinking != nil {
				gotType = out.Thinking.Type
			}
			if c.forbidThinking {
				if out.Thinking != nil {
					t.Errorf("thinking present for non-DeepSeek: %s", raw)
				}
				return
			}
			if gotType != c.wantThinking {
				t.Errorf("thinking.type = %q, want %q; body %s", gotType, c.wantThinking, raw)
			}
		})
	}
}

// Same inputs must marshal to the same bytes so prompt caches stay warm.
func TestBuildChatDeepSeekThinkingStableBytes(t *testing.T) {
	r := &Request{Thinking: true, Effort: "high", Messages: []Message{{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}}}}
	a := buildChat(r, "deepseek-chat", "api.deepseek.com", false)
	b := buildChat(r, "deepseek-chat", "api.deepseek.com", false)
	if !bytes.Equal(a, b) {
		t.Fatalf("unstable body:\n%s\n%s", a, b)
	}
	if !bytes.Contains(a, []byte(`"reasoning_effort":"high"`)) || !bytes.Contains(a, []byte(`"thinking":{"type":"enabled"}`)) {
		t.Fatalf("missing fields: %s", a)
	}
}

// Inbound OpenCode-style thinking:{type} is read into ThinkOff / Thinking
// even when reasoning_effort is absent (toggle).
func TestParseChatThinkingType(t *testing.T) {
	for _, c := range []struct {
		name, body string
		thinkOff   bool
		thinking   bool
		effort     string
	}{
		{"disabled alone", `{"model":"m","thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hi"}]}`, true, false, ""},
		{"enabled alone", `{"model":"m","thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hi"}]}`, false, true, ""},
		{"none wins over enabled", `{"model":"m","reasoning_effort":"none","thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hi"}]}`, true, true, "low"},
		{"high with enabled", `{"model":"m","reasoning_effort":"high","thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hi"}]}`, false, true, "high"},
		{"disabled with high effort", `{"model":"m","reasoning_effort":"high","thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hi"}]}`, true, true, "high"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, err := parseChat([]byte(c.body))
			if err != nil {
				t.Fatal(err)
			}
			if r.ThinkOff != c.thinkOff || r.Thinking != c.thinking || r.Effort != c.effort {
				t.Errorf("ThinkOff=%v Thinking=%v Effort=%q, want %v %v %q",
					r.ThinkOff, r.Thinking, r.Effort, c.thinkOff, c.thinking, c.effort)
			}
		})
	}
}

func TestWithEffortChatKeepsThinking(t *testing.T) {
	in := `{"model":"m","reasoning_effort":"low","thinking":{"type":"enabled"}}`
	got := string(withEffort(provider.Chat, []byte(in), "high"))
	if !strings.Contains(got, `"reasoning_effort":"high"`) || !strings.Contains(got, `"thinking":{"type":"enabled"}`) {
		t.Fatalf("got %s", got)
	}
	// thinking-only toggle: still treated as asked for reasoning
	in2 := `{"model":"m","thinking":{"type":"enabled"}}`
	got2 := string(withEffort(provider.Chat, []byte(in2), "medium"))
	if !strings.Contains(got2, `"thinking":{"type":"enabled"}`) {
		t.Fatalf("toggle: %s", got2)
	}
	if strings.Contains(got2, `"reasoning_effort"`) {
		t.Fatalf("toggle should not invent reasoning_effort: %s", got2)
	}
}

func TestWithFixedEffortChatKeepsThinking(t *testing.T) {
	in := `{"model":"m","reasoning_effort":"high","thinking":{"type":"enabled"}}`
	off := string(withFixedEffort(provider.Chat, []byte(in), "none"))
	if !strings.Contains(off, `"reasoning_effort":"none"`) || !strings.Contains(off, `"thinking":{"type":"disabled"}`) {
		t.Fatalf("none: %s", off)
	}
	on := string(withFixedEffort(provider.Chat, []byte(`{"model":"m","thinking":{"type":"disabled"}}`), "high"))
	if !strings.Contains(on, `"reasoning_effort":"high"`) || !strings.Contains(on, `"thinking":{"type":"enabled"}`) {
		t.Fatalf("high: %s", on)
	}
	// no thinking object: unchanged behaviour
	plain := string(withFixedEffort(provider.Chat, []byte(`{"model":"m"}`), "low"))
	if !strings.Contains(plain, `"reasoning_effort":"low"`) || strings.Contains(plain, `"thinking"`) {
		t.Fatalf("plain: %s", plain)
	}
}

func TestChatWantsThinkingType(t *testing.T) {
	for _, c := range []struct {
		host, model string
		want        bool
	}{
		{"api.deepseek.com", "deepseek-chat", true},
		{"opencode.ai", "deepseek-v4.1-flash", true},
		{"api.openai.com", "gpt-5", false},
		{"api.moonshot.cn", "kimi-k2", false},
		{provider.CommandCodePlanID, "some-model", false},
		{provider.CommandCodePlanID, "deepseek-v3", true},
	} {
		if got := chatWantsThinkingType(c.host, c.model); got != c.want {
			t.Errorf("chatWantsThinkingType(%q,%q)=%v, want %v", c.host, c.model, got, c.want)
		}
	}
}
