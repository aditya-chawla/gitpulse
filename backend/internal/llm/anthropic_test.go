package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// fakeAnthropic captures the request body and returns a canned Messages reply.
func fakeAnthropic(t *testing.T, stop string, captured *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SDK v2 sends the key as a bearer token.
		if r.URL.Path != "/v1/messages" || (r.Header.Get("Authorization") != "Bearer test-key" && r.Header.Get("X-Api-Key") != "test-key") {
			t.Errorf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-haiku-5-5",
			"stop_reason": stop,
			"content": []map[string]any{
				{"type": "thinking", "thinking": "", "signature": "s"},
				{"type": "text", "text": `{"summary":"ok"}`},
			},
			"usage": map[string]any{"input_tokens": 1200, "output_tokens": 300, "cache_read_input_tokens": 100},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAnthropicRequestShape(t *testing.T) {
	var body map[string]any
	srv := fakeAnthropic(t, "end_turn", &body)
	a := NewAnthropic(AnthropicConfig{APIKey: "test-key", Model: "claude-haiku-5-5", Effort: "low",
		Options: []option.RequestOption{option.WithBaseURL(srv.URL)}})
	resp, err := a.Complete(context.Background(), ProviderRequest{
		System: "sys", User: "user text", MaxTokens: 4000,
		Schema: map[string]any{"type": "object", "properties": map[string]any{"summary": map[string]any{"type": "string"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != `{"summary":"ok"}` || resp.InputTokens != 1300 || resp.OutputTokens != 300 || resp.Refused {
		t.Fatalf("resp = %+v", resp)
	}
	if body["model"] != "claude-haiku-5-5" || body["max_tokens"] != float64(4000) {
		t.Fatalf("model/max_tokens = %v %v", body["model"], body["max_tokens"])
	}
	oc, _ := body["output_config"].(map[string]any)
	if oc["effort"] != "low" {
		t.Errorf("effort = %v", oc["effort"])
	}
	if f, _ := oc["format"].(map[string]any); f["type"] != "json_schema" || f["schema"] == nil {
		t.Errorf("format = %v", oc["format"])
	}
	for _, forbidden := range []string{"tools", "tool_choice", "thinking", "temperature"} {
		if _, ok := body[forbidden]; ok {
			t.Errorf("request must not send %q", forbidden)
		}
	}
}

func TestAnthropicOmitsEffortForUnknownModel(t *testing.T) {
	var body map[string]any
	srv := fakeAnthropic(t, "end_turn", &body)
	a := NewAnthropic(AnthropicConfig{APIKey: "test-key", Model: "claude-future-9", Effort: "low",
		Options: []option.RequestOption{option.WithBaseURL(srv.URL)}})
	if _, err := a.Complete(context.Background(), ProviderRequest{System: "s", User: "u", MaxTokens: 100, Schema: map[string]any{"type": "object"}}); err != nil {
		t.Fatal(err)
	}
	if oc, _ := body["output_config"].(map[string]any); oc["effort"] != nil {
		t.Errorf("effort sent for a model not known to support it: %v", oc)
	}
}

func TestAnthropicRefusalAndTruncation(t *testing.T) {
	var body map[string]any
	a := NewAnthropic(AnthropicConfig{APIKey: "test-key", Model: "claude-haiku-5-5",
		Options: []option.RequestOption{option.WithBaseURL(fakeAnthropic(t, "refusal", &body).URL)}})
	resp, err := a.Complete(context.Background(), ProviderRequest{System: "s", User: "u", MaxTokens: 100, Schema: map[string]any{"type": "object"}})
	if err != nil || !resp.Refused || resp.OutputTokens != 300 {
		t.Fatalf("refusal: %+v %v", resp, err)
	}
	a = NewAnthropic(AnthropicConfig{APIKey: "test-key", Model: "claude-haiku-5-5",
		Options: []option.RequestOption{option.WithBaseURL(fakeAnthropic(t, "max_tokens", &body).URL)}})
	resp, err = a.Complete(context.Background(), ProviderRequest{System: "s", User: "u", MaxTokens: 100, Schema: map[string]any{"type": "object"}})
	if err == nil || resp.OutputTokens != 300 {
		t.Fatalf("truncation must error but still report usage: %+v %v", resp, err)
	}
}
