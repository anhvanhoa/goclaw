package tools

import (
	"context"
	"strings"
	"testing"
)

type mockCredentialProvider struct {
	apiKey  string
	apiBase string
}

func (m *mockCredentialProvider) APIKey() string  { return m.apiKey }
func (m *mockCredentialProvider) APIBase() string { return m.apiBase }

func TestReadVideo_BothMediaIdAndUrl_Error(t *testing.T) {
	tool := NewReadVideoTool(nil, nil)

	res := tool.Execute(context.Background(), map[string]any{
		"prompt":   "describe this video",
		"media_id": "video-123",
		"url":      "https://example.com/video.mp4",
	})

	if !res.IsError {
		t.Fatalf("expected error when both media_id and url are provided")
	}

	if !strings.Contains(res.ForLLM, "Both 'media_id' and 'url' parameters cannot be specified") {
		t.Errorf("unexpected error message: %s", res.ForLLM)
	}
}

func TestReadVideo_GeminiURL_Error(t *testing.T) {
	tool := NewReadVideoTool(nil, nil)

	params := map[string]any{
		"prompt":         "describe this video",
		"url":            "https://example.com/video.mp4",
		"_provider_type": "gemini",
	}

	cp := &mockCredentialProvider{apiKey: "test-key"}

	_, _, err := tool.callProvider(context.Background(), cp, "gemini", "gemini-2.5-flash", params)
	if err == nil {
		t.Fatalf("expected error for gemini native provider with video URL")
	}

	if !strings.Contains(err.Error(), "does not support analyzing videos directly from a URL") {
		t.Errorf("unexpected error message: %v", err)
	}
}
