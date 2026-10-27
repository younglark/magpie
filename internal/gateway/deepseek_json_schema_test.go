package gateway

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// grokMemoryBody is Grok Build's memory extraction request as it reaches
// Magpie for OpenCode Go's deepseek-v4.1-flash: Chat, streamed, a strict
// json_schema response_format, reasoning low, no tools.
const grokMemoryBody = `{"model":"deepseek-v4.1-flash","stream":true,"stream_options":{"include_usage":true},"max_tokens":16384,"reasoning_effort":"low",` +
	`"messages":[{"role":"system","content":"You extract durable memories."},{"role":"user","content":"Here is the session transcript."}],` +
	`"response_format":{"type":"json_schema","json_schema":{"name":"structured_output","strict":true,"schema":` + grokMemorySchema + `}}}`

const grokMemorySchema = `{"type":"object","additionalProperties":false,"required":["outcome","observations"],"properties":{` +
	`"outcome":{"enum":["noop","observations"]},` +
	`"observations":{"type":"array","items":{"type":"object","additionalProperties":false,` +
	`"required":["type","topic_hint","statement","keywords","aliases","body"],"properties":{` +
	`"type":{"enum":["user","feedback","project","reference"]},` +
	`"topic_hint":{"type":["string","null"]},` +
	`"statement":{"type":"string","maxLength":1024},` +
	`"keywords":{"type":"array","items":{"type":"string"}},` +
	`"aliases":{"type":"array","items":{"type":"string"}},` +
	`"body":{"type":["string","null"]}}}}}}`

func decodeChatOut(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, b)
	}
	return m
}

func lastMessage(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	msgs, _ := m["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("no messages: %v", m)
	}
	last, _ := msgs[len(msgs)-1].(map[string]any)
	return last
}

func checkSchemaLine(t *testing.T, content any) {
	t.Helper()
	s, _ := content.(string)
	if !strings.Contains(s, "json") {
		t.Fatalf("DeepSeek JSON Output wants the word json: %q", s)
	}
	if !strings.Contains(s, string(canonicalJSON(json.RawMessage(grokMemorySchema)))) {
		t.Fatalf("schema not told: %q", s)
	}
}

// The rebuilt Chat request for a DeepSeek model asks for json_object, the
// schema in a trailing system message; the client's system and user
// messages come first unchanged, so the cached prefix holds.
func TestBuildChatDeepSeekJSONSchemaAsObject(t *testing.T) {
	r, err := parseChat([]byte(grokMemoryBody))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ host, model string }{
		{"opencode.ai", "deepseek-v4.1-flash"},
		{"api.deepseek.com", "deepseek-chat"},
	} {
		m := decodeChatOut(t, buildChat(r, c.model, c.host, false))
		rf, _ := m["response_format"].(map[string]any)
		if len(rf) != 1 || rf["type"] != "json_object" {
			t.Fatalf("%s: response_format = %v, want only json_object", c.host, m["response_format"])
		}
		msgs := m["messages"].([]any)
		if len(msgs) != 3 {
			t.Fatalf("%s: messages = %v", c.host, msgs)
		}
		first := msgs[0].(map[string]any)
		if first["role"] != "system" || first["content"] != "You extract durable memories." {
			t.Fatalf("%s: client system changed: %v", c.host, first)
		}
		if u := msgs[1].(map[string]any); u["role"] != "user" {
			t.Fatalf("%s: second message %v", c.host, u)
		}
		last := lastMessage(t, m)
		if last["role"] != "system" {
			t.Fatalf("%s: last message %v", c.host, last)
		}
		checkSchemaLine(t, last["content"])
		if m["reasoning_effort"] != "low" {
			t.Fatalf("%s: reasoning_effort = %v", c.host, m["reasoning_effort"])
		}
	}
}

// A non-DeepSeek Chat upstream still gets the json_schema as the client
// asked, with no added message.
func TestBuildChatOtherJSONSchemaUnchanged(t *testing.T) {
	r, err := parseChat([]byte(grokMemoryBody))
	if err != nil {
		t.Fatal(err)
	}
	m := decodeChatOut(t, buildChat(r, "gpt-5.4", "api.openai.com", false))
	rf, _ := m["response_format"].(map[string]any)
	if rf["type"] != "json_schema" {
		t.Fatalf("response_format = %v", rf)
	}
	js, _ := rf["json_schema"].(map[string]any)
	if js["name"] != "structured_output" || js["strict"] != true {
		t.Fatalf("json_schema = %v", js)
	}
	if n := len(m["messages"].([]any)); n != 2 {
		t.Fatalf("messages = %d, want 2", n)
	}
}

// A json_object request to DeepSeek is left as it is: no schema to tell.
func TestBuildChatDeepSeekJSONObjectUntouched(t *testing.T) {
	body := `{"model":"deepseek-chat","messages":[{"role":"user","content":"give json"}],"response_format":{"type":"json_object"}}`
	r, err := parseChat([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	m := decodeChatOut(t, buildChat(r, "deepseek-chat", "api.deepseek.com", false))
	if rf := m["response_format"].(map[string]any); rf["type"] != "json_object" {
		t.Fatalf("response_format = %v", rf)
	}
	if n := len(m["messages"].([]any)); n != 1 {
		t.Fatalf("messages = %d, want 1", n)
	}
}

// The schema line is the same bytes however the client ordered the
// schema's keys, and the built request is byte-stable.
func TestDeepSeekSchemaLineStable(t *testing.T) {
	a := deepseekSchemaLine(json.RawMessage(`{"type":"object","required":["a"],"properties":{"b":{"type":"string"},"a":{"maxLength":1024,"type":"string"}}}`))
	b := deepseekSchemaLine(json.RawMessage(`{"properties":{"a":{"type":"string","maxLength":1024},"b":{"type":"string"}},"required":["a"],"type":"object"}`))
	if a != b {
		t.Fatalf("not stable:\n%s\n%s", a, b)
	}
	if strings.Contains(a, `\u003c`) {
		t.Fatalf("HTML-escaped: %s", a)
	}
	r, _ := parseChat([]byte(grokMemoryBody))
	one := buildChat(r, "deepseek-v4.1-flash", "opencode.ai", false)
	two := buildChat(r, "deepseek-v4.1-flash", "opencode.ai", false)
	if !bytes.Equal(one, two) {
		t.Fatal("buildChat not byte-stable")
	}
}

// The body passed through to a DeepSeek Chat upstream: json_object, the
// schema in a trailing system message, everything else as sent.
func TestDeepSeekFormatBodyPassthrough(t *testing.T) {
	nb, ok := deepseekFormatBody([]byte(grokMemoryBody))
	if !ok {
		t.Fatal("not changed")
	}
	m := decodeChatOut(t, nb)
	if rf := m["response_format"].(map[string]any); len(rf) != 1 || rf["type"] != "json_object" {
		t.Fatalf("response_format = %v", rf)
	}
	msgs := m["messages"].([]any)
	if len(msgs) != 3 || msgs[0].(map[string]any)["content"] != "You extract durable memories." {
		t.Fatalf("messages = %v", msgs)
	}
	checkSchemaLine(t, lastMessage(t, m)["content"])
	if m["reasoning_effort"] != "low" || m["stream"] != true || m["max_tokens"] != float64(16384) {
		t.Fatalf("other fields changed: %v", m)
	}
	if so := m["stream_options"].(map[string]any); so["include_usage"] != true {
		t.Fatalf("stream_options = %v", so)
	}
	again, _ := deepseekFormatBody([]byte(grokMemoryBody))
	if !bytes.Equal(nb, again) {
		t.Fatal("not byte-stable")
	}
	// nothing to do for json_object or no format
	for _, b := range []string{
		`{"model":"deepseek-chat","messages":[],"response_format":{"type":"json_object"}}`,
		`{"model":"deepseek-chat","messages":[]}`,
	} {
		if out, ok := deepseekFormatBody([]byte(b)); ok || string(out) != b {
			t.Fatalf("changed %s → %s", b, out)
		}
	}
}

func TestChatTakesObjectOnly(t *testing.T) {
	for _, c := range []struct {
		host, model string
		want        bool
	}{
		{"api.deepseek.com", "deepseek-chat", true},
		{"opencode.ai", "deepseek-v4.1-flash", true},
		{"openrouter.ai", "deepseek/deepseek-v4", true},
		{"api.openai.com", "gpt-5.4", false},
		{"api.moonshot.ai", "kimi-k2", false},
	} {
		if got := chatTakesObjectOnly(c.host, c.model); got != c.want {
			t.Fatalf("%s %s: %v", c.host, c.model, got)
		}
	}
}

// Grok Build's memory extraction (Chat, passed through) and a Responses
// client's schema (built again for Chat) reach a DeepSeek model asking
// for json_object from the first request, the schema told once in a
// trailing system message: no 400 to recover from, the client's own
// messages first as they were.
func TestDeepSeekSchemaAskedAsObjectUpfront(t *testing.T) {
	for _, c := range []struct{ name, path, body, firstSystem string }{
		{"chat passthrough", "/v1/chat/completions", strings.Replace(grokMemoryBody, `"deepseek-v4.1-flash"`, `"ds/deepseek-v4.1-flash"`, 1), "You extract durable memories."},
		{"responses rebuilt", "/v1/responses", strings.Replace(sdkResponsesParse, "%s", "ds/deepseek-v4.1-flash", 1), "Extract the event information."},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			up := &formatRefuser{}
			srv := httptest.NewServer(up)
			t.Cleanup(srv.Close)
			if err := provider.Save(provider.Provider{ID: "ds", Name: "OpenCode Go", Key: "k", Chat: srv.URL + "/v1", Models: []string{"deepseek-v4.1-flash"}}); err != nil {
				t.Fatal(err)
			}
			h := New().Handler()
			for i := range 2 {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", c.path, strings.NewReader(c.body)))
				if rec.Code != 200 {
					t.Fatalf("turn %d: status %d: %s", i, rec.Code, rec.Body.String())
				}
				bodies := up.all()
				if len(bodies) != i+1 {
					t.Fatalf("turn %d: %d upstream requests, want one per turn", i, len(bodies))
				}
				sent := bodies[i]
				if jsonOf(sent["response_format"]) != `{"type":"json_object"}` {
					t.Fatalf("turn %d: response_format %s", i, jsonOf(sent["response_format"]))
				}
				msgs, _ := sent["messages"].([]any)
				first, _ := msgs[0].(map[string]any)
				if first["role"] != "system" || first["content"] != c.firstSystem {
					t.Fatalf("turn %d: first message changed: %s", i, jsonOf(first))
				}
				last, _ := msgs[len(msgs)-1].(map[string]any)
				line, _ := last["content"].(string)
				if last["role"] != "system" || !strings.Contains(line, "json object") || !strings.Contains(line, "matching this JSON schema") {
					t.Fatalf("turn %d: schema not told last: %s", i, jsonOf(msgs))
				}
				if n := strings.Count(jsonOf(msgs), "matching this JSON schema"); n != 1 {
					t.Fatalf("turn %d: schema told %d times", i, n)
				}
			}
		})
	}
}
