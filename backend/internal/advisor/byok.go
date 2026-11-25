package advisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (s *Service) callBYOK(ctx context.Context, provider, model, apiKey, sysPrompt, userPrompt string) (string, string, error) {
	model = strings.TrimSpace(model)
	switch provider {
	case "gemini":
		if model == "" {
			ans, err := s.callGemini(ctx, "gemini-3.8-flash", apiKey, sysPrompt, userPrompt)
			if err == nil {
				return ans, "gemini-3.8-flash", nil
			}
			// Fallback for older keys/projects that still serve gemini-2.5-flash.
			if strings.Contains(err.Error(), "HTTP 404") {
				if ans2, err2 := s.callGemini(ctx, "gemini-2.5-flash", apiKey, sysPrompt, userPrompt); err2 == nil {
					return ans2, "gemini-2.5-flash", nil
				}
			}
			return "", "gemini-3.8-flash", err
		}
		model = strings.TrimPrefix(model, "models/")
		ans, err := s.callGemini(ctx, model, apiKey, sysPrompt, userPrompt)
		return ans, model, err
	case "openai":
		if strings.TrimSpace(model) == "" {
			model = "gpt-4o-mini"
		}
		ans, err := s.callOpenAI(ctx, model, apiKey, sysPrompt, userPrompt)
		return ans, model, err
	case "anthropic":
		if strings.TrimSpace(model) == "" {
			model = "claude-haiku-4-5-20251001"
		}
		ans, err := s.callAnthropic(ctx, model, apiKey, sysPrompt, userPrompt)
		return ans, model, err
	default:
		return "", "", fmt.Errorf("unsupported provider %q", provider)
	}
}

func (s *Service) callGemini(ctx context.Context, model, apiKey, sysPrompt, userPrompt string) (string, error) {
	body := map[string]any{
		"systemInstruction": map[string]any{
			"parts": []map[string]string{{"text": sysPrompt}},
		},
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": userPrompt}}},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": 900,
			"temperature":     0.3,
		},
	}
	url := fmt.Sprintf("%s/%s:generateContent", strings.TrimRight(s.geminiBaseURL, "/"), model)
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := s.postJSON(ctx, url, map[string]string{"x-goog-api-key": apiKey}, body, &out); err != nil {
		return "", err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return strings.TrimSpace(out.Candidates[0].Content.Parts[0].Text), nil
}

func (s *Service) callOpenAI(ctx context.Context, model, apiKey, sysPrompt, userPrompt string) (string, error) {
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": userPrompt},
		},
		"max_tokens":  900,
		"temperature": 0.3,
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := s.postJSON(ctx, s.openAIURL, map[string]string{"Authorization": "Bearer " + apiKey}, body, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("empty response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func (s *Service) callAnthropic(ctx context.Context, model, apiKey, sysPrompt, userPrompt string) (string, error) {
	body := map[string]any{
		"model":      model,
		"max_tokens": 900,
		"system":     sysPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userPrompt},
		},
	}
	headers := map[string]string{
		"x-api-key":         apiKey,
		"anthropic-version": "2023-06-01",
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := s.postJSON(ctx, s.anthropicBaseURL, headers, body, &out); err != nil {
		return "", err
	}
	if len(out.Content) == 0 || strings.TrimSpace(out.Content[0].Text) == "" {
		return "", fmt.Errorf("empty response")
	}
	return strings.TrimSpace(out.Content[0].Text), nil
}

func (s *Service) postJSON(ctx context.Context, url string, headers map[string]string, payload any, dest any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(respBytes, &errBody) == nil && errBody.Error.Message != "" {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, errBody.Error.Message)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(respBytes, dest)
}
