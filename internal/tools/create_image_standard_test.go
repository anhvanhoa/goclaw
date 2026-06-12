package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallStandardImageGenAPI_GPTImageOmitsResponseFormat(t *testing.T) {
	wantPNG := []byte{0x89, 0x50, 0x4e, 0x47}
	b64 := base64.StdEncoding.EncodeToString(wantPNG)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("path = %q, want /v1/images/generations", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"b64_json": b64}},
		})
	}))
	defer srv.Close()

	tool := NewCreateImageTool(nil)
	out, usage, err := tool.callStandardImageGenAPI(context.Background(), "test-key", srv.URL+"/v1", "gpt-image-1.5", "a prompt",
		map[string]any{"output_format": "webp"})
	if err != nil {
		t.Fatalf("callStandardImageGenAPI: %v", err)
	}
	if usage != nil {
		t.Fatalf("usage = %#v, want nil", usage)
	}
	if string(out) != string(wantPNG) {
		t.Fatalf("decoded bytes mismatch")
	}
	if _, ok := body["response_format"]; ok {
		t.Fatalf("request must not include response_format for gpt-image models: %#v", body)
	}
	if body["output_format"] != "webp" {
		t.Fatalf("output_format = %v, want webp", body["output_format"])
	}
}

func TestCallStandardImageGenAPI_LegacyImageOmitsResponseFormat(t *testing.T) {
	wantPNG := []byte{0x89, 0x50, 0x4e, 0x47}
	b64 := base64.StdEncoding.EncodeToString(wantPNG)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"b64_json": b64}},
		})
	}))
	defer srv.Close()

	tool := NewCreateImageTool(nil)
	_, _, err := tool.callStandardImageGenAPI(context.Background(), "test-key", srv.URL+"/v1", "dall-e-3", "a prompt", nil)
	if err != nil {
		t.Fatalf("callStandardImageGenAPI: %v", err)
	}
	if _, ok := body["response_format"]; ok {
		t.Fatalf("request must not include response_format: %#v", body)
	}
	if _, ok := body["output_format"]; ok {
		t.Fatalf("legacy request must not include output_format: %#v", body)
	}
}

func TestCallStandardImageGenAPI_URLResponseFallback(t *testing.T) {
	wantPNG := []byte{0x89, 0x50, 0x4e, 0x47}

	imageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(wantPNG)
	}))
	defer imageSrv.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"url": imageSrv.URL + "/image.png"}},
		})
	}))
	defer srv.Close()

	tool := NewCreateImageTool(nil)
	out, usage, err := tool.callStandardImageGenAPI(context.Background(), "test-key", srv.URL+"/v1", "dall-e-3", "a prompt", nil)
	if err != nil {
		t.Fatalf("callStandardImageGenAPI: %v", err)
	}
	if usage != nil {
		t.Fatalf("usage = %#v, want nil", usage)
	}
	if string(out) != string(wantPNG) {
		t.Fatalf("downloaded bytes mismatch")
	}
}
