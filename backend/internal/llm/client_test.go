package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aiopencad/backend/internal/config"
)

func TestCleanupCodeStripsFence(t *testing.T) {
	code := cleanupCode("```js\nBox(1, 2, 3, true);\n```")
	if code != "Box(1, 2, 3, true);" {
		t.Fatalf("unexpected code: %q", code)
	}
}

func TestExtractJSONObject(t *testing.T) {
	raw := "prefix ```json\n{\"code\":\"Box(1,2,3,true);\"}\n``` suffix"
	got := extractJSONObject(raw)
	if got != `{"code":"Box(1,2,3,true);"}` {
		t.Fatalf("unexpected json: %s", got)
	}
}

func TestResponsesEndpoint(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com":                     "https://api.example.com/v1/responses",
		"https://api.example.com/v1":                  "https://api.example.com/v1/responses",
		"https://api.example.com/v1/responses":        "https://api.example.com/v1/responses",
		"https://api.example.com/v1/chat/completions": "https://api.example.com/v1/responses",
	}
	for input, want := range cases {
		if got := responsesEndpoint(input); got != want {
			t.Fatalf("endpoint mismatch for %s: got %s want %s", input, got, want)
		}
	}
}

func TestChatCompletionsEndpoint(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com":                     "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1":                  "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1/responses":        "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1/chat/completions": "https://api.example.com/v1/chat/completions",
	}
	for input, want := range cases {
		if got := chatCompletionsEndpoint(input); got != want {
			t.Fatalf("endpoint mismatch for %s: got %s want %s", input, got, want)
		}
	}
}

func TestDemoGenerate(t *testing.T) {
	resp := demoGenerate(GenerateRequest{Prompt: "make a flange", Language: "cascade-js"})
	if resp.Code == "" || len(resp.Warnings) == 0 {
		t.Fatalf("expected demo code and warning: %+v", resp)
	}
	if resp.Code == "cube([1,2,3]);" {
		t.Fatal("expected Cascade Studio JS, got OpenSCAD-looking code")
	}
}

func TestGenerateCADCallsOpenAIResponsesAPI(t *testing.T) {
	var seen responsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(responsesResponse{
			OutputText: `{"code":"Box(10,20,30,true);","explanation":"ok","warnings":[]}`,
		})
	}))
	defer server.Close()

	client := NewClient(config.LLMConfig{
		BaseURL:          server.URL,
		APIKey:           "test-key",
		Model:            "test-model",
		Timeout:          time.Second,
		ReasoningEffort:  "xhigh",
		EnableWebSearch:  true,
		RequireWebSearch: true,
	})
	resp, err := client.GenerateCAD(context.Background(), GenerateRequest{
		Prompt:   "make a box",
		Language: "cascade-js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen.Model != "test-model" {
		t.Fatalf("unexpected model: %s", seen.Model)
	}
	if seen.Reasoning == nil || seen.Reasoning.Effort != "xhigh" {
		t.Fatalf("unexpected reasoning config: %+v", seen.Reasoning)
	}
	if len(seen.Tools) != 1 || seen.Tools[0].Type != "web_search" {
		t.Fatalf("unexpected tools: %+v", seen.Tools)
	}
	if seen.ToolChoice != "required" {
		t.Fatalf("expected required tool choice, got %q", seen.ToolChoice)
	}
	if seen.Text == nil || seen.Text.Format == nil || seen.Text.Format.Type != "json_schema" {
		t.Fatalf("unexpected text format: %+v", seen.Text)
	}
	if resp.Code != "Box(10,20,30,true);" {
		t.Fatalf("unexpected code: %s", resp.Code)
	}
}

func TestGenerateCADFromImageCallsResponsesAPIWithImage(t *testing.T) {
	var seen responsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(responsesResponse{
			OutputText: `{"code":"Box(10,20,30,true);","explanation":"from image","warnings":["assumed thickness"]}`,
		})
	}))
	defer server.Close()

	client := NewClient(config.LLMConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		Model:           "test-model",
		Timeout:         time.Second,
		ReasoningEffort: "high",
	})
	resp, err := client.GenerateCADFromImage(context.Background(), GenerateFromImageRequest{
		Prompt:   "make this bracket",
		Language: "cascade-js",
		Image:    []byte{0x89, 0x50, 0x4e, 0x47},
		MimeType: "image/png",
		FileName: "bracket.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Code != "Box(10,20,30,true);" {
		t.Fatalf("unexpected code: %s", resp.Code)
	}
	if len(seen.Input) != 2 {
		t.Fatalf("unexpected input length: %+v", seen.Input)
	}
	user := seen.Input[1]
	if user.Role != "user" || len(user.Content) != 2 {
		t.Fatalf("unexpected user content: %+v", user)
	}
	if user.Content[1].Type != "input_image" {
		t.Fatalf("expected input_image, got %+v", user.Content[1])
	}
	if !strings.HasPrefix(user.Content[1].ImageURL, "data:image/png;base64,") {
		t.Fatalf("expected image data URL, got %s", user.Content[1].ImageURL)
	}
}

func TestRefineCADCallsOpenAIResponsesAPI(t *testing.T) {
	var seen responsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(responsesResponse{
			Output: []struct {
				Type    string `json:"type"`
				Role    string `json:"role,omitempty"`
				Content []struct {
					Type    string `json:"type"`
					Text    string `json:"text,omitempty"`
					Refusal string `json:"refusal,omitempty"`
				} `json:"content,omitempty"`
			}{
				{
					Type: "message",
					Role: "assistant",
					Content: []struct {
						Type    string `json:"type"`
						Text    string `json:"text,omitempty"`
						Refusal string `json:"refusal,omitempty"`
					}{
						{Type: "output_text", Text: `{"code":"Box(20,20,30,true);","changes":["increased height"]}`},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(config.LLMConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		Model:           "test-model",
		Timeout:         time.Second,
		ReasoningEffort: "high",
	})
	resp, err := client.RefineCAD(context.Background(), RefineRequest{
		Prompt:      "make a box",
		Code:        "Box(20,20,20,true);",
		Instruction: "make it taller",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen.Input) != 2 {
		t.Fatalf("unexpected input: %+v", seen.Input)
	}
	if resp.Code != "Box(20,20,30,true);" {
		t.Fatalf("unexpected code: %s", resp.Code)
	}
	if len(resp.Changes) != 1 {
		t.Fatalf("expected changes, got %+v", resp.Changes)
	}
}

func TestGenerateCADCanDisableReasoningAndWebSearch(t *testing.T) {
	var seen responsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(responsesResponse{
			OutputText: `{"code":"Sphere(10);","explanation":"ok","warnings":[]}`,
		})
	}))
	defer server.Close()

	client := NewClient(config.LLMConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		Model:           "test-model",
		Timeout:         time.Second,
		ReasoningEffort: "none",
		EnableWebSearch: false,
	})
	resp, err := client.GenerateCAD(context.Background(), GenerateRequest{
		Prompt:   "make a sphere",
		Language: "cascade-js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen.Reasoning != nil {
		t.Fatalf("expected no reasoning config, got %+v", seen.Reasoning)
	}
	if len(seen.Tools) != 0 {
		t.Fatalf("expected no tools, got %+v", seen.Tools)
	}
	if resp.Code != "Sphere(10);" {
		t.Fatalf("unexpected code: %s", resp.Code)
	}
}

func TestGenerateCADFallsBackToChatCompletions(t *testing.T) {
	var paths []string
	var seenChat chatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/responses" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"Upstream request failed","type":"upstream_error"}}`))
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenChat); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []struct {
				Message chatMessage `json:"message"`
			}{
				{
					Message: chatMessage{
						Role:    "assistant",
						Content: `{"code":"Box(8,8,8,true);","explanation":"chat fallback","warnings":[]}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(config.LLMConfig{
		BaseURL:          server.URL,
		APIKey:           "test-key",
		Model:            "test-model",
		Timeout:          time.Second,
		ReasoningEffort:  "xhigh",
		EnableWebSearch:  true,
		RequireWebSearch: false,
	})
	resp, err := client.GenerateCAD(context.Background(), GenerateRequest{
		Prompt:   "make a cube",
		Language: "cascade-js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Code != "Box(8,8,8,true);" {
		t.Fatalf("unexpected code: %s", resp.Code)
	}
	if len(paths) != 8 {
		t.Fatalf("expected six responses attempts plus streamed and non-streamed chat fallback, got paths: %+v", paths)
	}
	if seenChat.ResponseFormat == nil || seenChat.ResponseFormat.Type != "json_object" {
		t.Fatalf("expected json_object chat fallback, got %+v", seenChat.ResponseFormat)
	}
}

func TestGenerateCADRequiredWebSearchDoesNotFallBackToChat(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"unsupported tool: web_search"}}`))
	}))
	defer server.Close()

	client := NewClient(config.LLMConfig{
		BaseURL:          server.URL,
		APIKey:           "test-key",
		Model:            "test-model",
		Timeout:          time.Second,
		ReasoningEffort:  "xhigh",
		EnableWebSearch:  true,
		RequireWebSearch: true,
	})
	_, err := client.GenerateCAD(context.Background(), GenerateRequest{
		Prompt:   "make a cube with verified dimensions",
		Language: "cascade-js",
	})
	if err == nil {
		t.Fatal("expected required web search error")
	}
	if !strings.Contains(err.Error(), "required web search is not available") {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, path := range paths {
		if path == "/v1/chat/completions" {
			t.Fatalf("required web search should not fall back to chat completions: %+v", paths)
		}
	}
}

func TestGenerateCADStreamsResponsesDeltas(t *testing.T) {
	var seen responsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.output_text.delta","delta":"{\"code\":\"Box(1,1,1,true);\""}` + "\n\n"))
		_, _ = w.Write([]byte("event: response.output_text.delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.output_text.delta","delta":",\"explanation\":\"ok\",\"warnings\":[]}"}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	var progress []string
	ctx := WithProgress(context.Background(), func(message string) {
		progress = append(progress, message)
	})
	client := NewClient(config.LLMConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		Model:           "test-model",
		Timeout:         time.Second,
		ReasoningEffort: "xhigh",
	})
	resp, err := client.GenerateCAD(ctx, GenerateRequest{
		Prompt:   "make a tiny cube",
		Language: "cascade-js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !seen.Stream {
		t.Fatal("expected streaming responses request")
	}
	if resp.Code != "Box(1,1,1,true);" {
		t.Fatalf("unexpected code: %s", resp.Code)
	}
	if !strings.Contains(strings.Join(progress, "\n"), "MODEL_DELTA") {
		t.Fatalf("expected streamed progress delta, got %+v", progress)
	}
}

func TestReadResponsesStreamUsesSSEEventNameWhenTypeMissing(t *testing.T) {
	var progress []string
	ctx := WithProgress(context.Background(), func(message string) {
		progress = append(progress, message)
	})

	content, err := readResponsesStream(ctx, strings.NewReader(strings.Join([]string{
		"event: response.output_text.delta",
		`data: {"delta":"hello "}`,
		"",
		"event: response.output_text.delta",
		`data: {"delta":"world"}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if content != "hello world" {
		t.Fatalf("unexpected stream content: %q", content)
	}
	if got := strings.Join(progress, "\n"); !strings.Contains(got, "MODEL_DELTA hello ") || !strings.Contains(got, "MODEL_DELTA world") {
		t.Fatalf("expected model deltas in progress, got %+v", progress)
	}
}

func TestReadResponsesStreamExtractsCompletedResponseOutput(t *testing.T) {
	content, err := readResponsesStream(context.Background(), strings.NewReader(strings.Join([]string{
		"event: response.completed",
		`data: {"response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"code\":\"Box(2,2,2,true);\",\"explanation\":\"ok\",\"warnings\":[]}"}]}]}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, `"code":"Box(2,2,2,true);"`) {
		t.Fatalf("unexpected completed content: %q", content)
	}
}
