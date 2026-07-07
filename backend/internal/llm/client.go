package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"aiopencad/backend/internal/config"
)

const defaultModel = "gpt-5.5"

type Client struct {
	cfg        config.LLMConfig
	httpClient *http.Client
}

type progressContextKey struct{}

type progressFunc func(string)

func WithProgress(ctx context.Context, report func(string)) context.Context {
	if report == nil {
		return ctx
	}
	return context.WithValue(ctx, progressContextKey{}, progressFunc(report))
}

func reportProgress(ctx context.Context, message string) {
	if message == "" {
		return
	}
	report, ok := ctx.Value(progressContextKey{}).(progressFunc)
	if ok && report != nil {
		report(message)
	}
}

type GenerateRequest struct {
	Prompt    string `json:"prompt"`
	Language  string `json:"language"`
	ProjectID string `json:"projectId,omitempty"`
}

type GenerateResponse struct {
	Code        string   `json:"code"`
	Explanation string   `json:"explanation"`
	Warnings    []string `json:"warnings"`
}

type GenerateFromImageRequest struct {
	Prompt    string
	Language  string
	Image     []byte
	MimeType  string
	FileName  string
	ProjectID string
}

type RepairRequest struct {
	Prompt string   `json:"prompt"`
	Code   string   `json:"code"`
	Error  string   `json:"error"`
	Logs   []string `json:"logs"`
}

type RepairResponse struct {
	Code    string   `json:"code"`
	Changes []string `json:"changes"`
}

type RefineRequest struct {
	Prompt      string `json:"prompt"`
	Code        string `json:"code"`
	Instruction string `json:"instruction"`
}

type RefineResponse struct {
	Code    string   `json:"code"`
	Changes []string `json:"changes"`
}

type modelMessage struct {
	Role    string
	Content string
}

type responsesRequest struct {
	Model     string              `json:"model"`
	Input     []responseInput     `json:"input"`
	Reasoning *responsesReasoning `json:"reasoning,omitempty"`
	Tools     []responsesTool     `json:"tools,omitempty"`
	Text      *responsesText      `json:"text,omitempty"`
	Stream    bool                `json:"stream,omitempty"`
}

type responseInput struct {
	Role    string                `json:"role"`
	Content []responseContentPart `json:"content"`
}

type responseContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responsesTool struct {
	Type string `json:"type"`
}

type responsesText struct {
	Format *responsesTextFormat `json:"format,omitempty"`
}

type responsesTextFormat struct {
	Type   string         `json:"type"`
	Name   string         `json:"name,omitempty"`
	Strict bool           `json:"strict,omitempty"`
	Schema map[string]any `json:"schema,omitempty"`
}

type responsesResponse struct {
	OutputText string `json:"output_text,omitempty"`
	Output     []struct {
		Type    string `json:"type"`
		Role    string `json:"role,omitempty"`
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text,omitempty"`
			Refusal string `json:"refusal,omitempty"`
		} `json:"content,omitempty"`
	} `json:"output,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type,omitempty"`
		Code    string `json:"code,omitempty"`
	} `json:"error,omitempty"`
}

type chatCompletionRequest struct {
	Model          string              `json:"model"`
	Messages       []chatMessage       `json:"messages"`
	Temperature    float64             `json:"temperature"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
	Stream         bool                `json:"stream,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponseFormat struct {
	Type string `json:"type"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type,omitempty"`
		Code    string `json:"code,omitempty"`
	} `json:"error,omitempty"`
}

type llmHTTPError struct {
	status int
	body   string
}

func (e llmHTTPError) Error() string {
	return fmt.Sprintf("llm request failed: status=%d body=%s", e.status, e.body)
}

func NewClient(cfg config.LLMConfig) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) doHTTPRequest(req *http.Request, timeout time.Duration) (*http.Response, error) {
	if timeout <= 0 || timeout == c.httpClient.Timeout {
		return c.httpClient.Do(req)
	}
	client := *c.httpClient
	client.Timeout = timeout
	return client.Do(req)
}

func (c *Client) GenerateCAD(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		return GenerateResponse{}, errors.New("prompt is required")
	}
	req.Language = "cascade-js"
	cfg := c.effectiveConfig()
	if cfg.APIKey == "" {
		reportProgress(ctx, "LLM API key is empty; using built-in demo CAD response.")
		return demoGenerate(req), nil
	}

	reportProgress(ctx, "Preparing CAD generation request.")
	content, err := c.callModel(ctx, cfg, "generate_cad", generateCADJSONSchema(), []modelMessage{
		{Role: "system", Content: generateSystemPrompt()},
		{Role: "user", Content: req.Prompt},
	})
	if err != nil {
		return GenerateResponse{}, err
	}

	reportProgress(ctx, "Parsing model response.")
	var out GenerateResponse
	if err := decodeJSONContent(content, &out); err != nil {
		return GenerateResponse{}, err
	}
	out.Code = cleanupCode(out.Code)
	if strings.TrimSpace(out.Code) == "" {
		return GenerateResponse{}, errors.New("model returned empty code")
	}
	reportProgress(ctx, "Generated Cascade Studio JS successfully.")
	return out, nil
}

func (c *Client) GenerateCADFromImage(ctx context.Context, req GenerateFromImageRequest) (GenerateResponse, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		req.Prompt = "Generate a practical CAD model from this image."
	}
	req.Language = "cascade-js"
	if len(req.Image) == 0 {
		return GenerateResponse{}, errors.New("image is required")
	}
	mimeType, ok := normalizeImageMimeType(req.MimeType)
	if !ok {
		return GenerateResponse{}, fmt.Errorf("unsupported image MIME type: %s", req.MimeType)
	}

	cfg := c.effectiveConfig()
	if cfg.APIKey == "" {
		reportProgress(ctx, "LLM API key is empty; using built-in demo CAD response without image analysis.")
		out := demoGenerate(GenerateRequest{Prompt: req.Prompt, Language: "cascade-js"})
		out.Warnings = append([]string{"Image analysis requires llm.apiKey; demo mode cannot inspect the uploaded image."}, out.Warnings...)
		return out, nil
	}

	reportProgress(ctx, "Preparing image-to-CAD vision request.")
	input := responseInputMessages([]modelMessage{
		{Role: "system", Content: generateImageSystemPrompt()},
	})
	input = append(input, responseInput{
		Role: "user",
		Content: []responseContentPart{
			{Type: "input_text", Text: imageCADUserPrompt(req)},
			{Type: "input_image", ImageURL: imageDataURL(mimeType, req.Image), Detail: "high"},
		},
	})

	content, err := c.callResponsesInput(ctx, cfg, "generate_cad_from_image", generateCADJSONSchema(), input)
	if err != nil {
		return GenerateResponse{}, err
	}

	reportProgress(ctx, "Parsing image-to-CAD model response.")
	var out GenerateResponse
	if err := decodeJSONContent(content, &out); err != nil {
		return GenerateResponse{}, err
	}
	out.Code = cleanupCode(out.Code)
	if strings.TrimSpace(out.Code) == "" {
		return GenerateResponse{}, errors.New("model returned empty code")
	}
	reportProgress(ctx, "Generated Cascade Studio JS from image successfully.")
	return out, nil
}

func (c *Client) RepairCAD(ctx context.Context, req RepairRequest) (RepairResponse, error) {
	if strings.TrimSpace(req.Code) == "" {
		return RepairResponse{}, errors.New("code is required")
	}
	cfg := c.effectiveConfig()
	if cfg.APIKey == "" {
		reportProgress(ctx, "LLM API key is empty; using built-in demo repair.")
		return RepairResponse{
			Code:    demoGenerate(GenerateRequest{Prompt: req.Prompt, Language: "cascade-js"}).Code,
			Changes: []string{"llm.apiKey is not configured, so the backend returned a stable demo model."},
		}, nil
	}

	reportProgress(ctx, "Preparing CAD repair request.")
	payload := map[string]any{
		"prompt": req.Prompt,
		"code":   req.Code,
		"error":  req.Error,
		"logs":   req.Logs,
	}
	raw, _ := json.MarshalIndent(payload, "", "  ")
	content, err := c.callModel(ctx, cfg, "repair_cad", repairCADJSONSchema(), []modelMessage{
		{Role: "system", Content: repairSystemPrompt()},
		{Role: "user", Content: string(raw)},
	})
	if err != nil {
		return RepairResponse{}, err
	}

	reportProgress(ctx, "Parsing repaired model response.")
	var out RepairResponse
	if err := decodeJSONContent(content, &out); err != nil {
		return RepairResponse{}, err
	}
	out.Code = cleanupCode(out.Code)
	if strings.TrimSpace(out.Code) == "" {
		return RepairResponse{}, errors.New("model returned empty repaired code")
	}
	reportProgress(ctx, "Repaired Cascade Studio JS successfully.")
	return out, nil
}

func (c *Client) RefineCAD(ctx context.Context, req RefineRequest) (RefineResponse, error) {
	req.Instruction = strings.TrimSpace(req.Instruction)
	if req.Instruction == "" {
		return RefineResponse{}, errors.New("instruction is required")
	}
	if strings.TrimSpace(req.Code) == "" {
		return RefineResponse{}, errors.New("code is required")
	}
	cfg := c.effectiveConfig()
	if cfg.APIKey == "" {
		reportProgress(ctx, "LLM API key is empty; using built-in demo refinement.")
		return RefineResponse{
			Code:    demoGenerate(GenerateRequest{Prompt: req.Prompt + " " + req.Instruction, Language: "cascade-js"}).Code,
			Changes: []string{"llm.apiKey is not configured, so the backend returned a stable demo refinement."},
		}, nil
	}

	reportProgress(ctx, "Preparing CAD refinement request.")
	payload := map[string]any{
		"originalPrompt": req.Prompt,
		"currentCode":    req.Code,
		"instruction":    req.Instruction,
	}
	raw, _ := json.MarshalIndent(payload, "", "  ")
	content, err := c.callModel(ctx, cfg, "refine_cad", refineCADJSONSchema(), []modelMessage{
		{Role: "system", Content: refineSystemPrompt()},
		{Role: "user", Content: string(raw)},
	})
	if err != nil {
		return RefineResponse{}, err
	}

	reportProgress(ctx, "Parsing refined model response.")
	var out RefineResponse
	if err := decodeJSONContent(content, &out); err != nil {
		return RefineResponse{}, err
	}
	out.Code = cleanupCode(out.Code)
	if strings.TrimSpace(out.Code) == "" {
		return RefineResponse{}, errors.New("model returned empty refined code")
	}
	reportProgress(ctx, "Refined Cascade Studio JS successfully.")
	return out, nil
}

func (c *Client) effectiveConfig() config.LLMConfig {
	cfg := c.cfg
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	return cfg
}

func (c *Client) callModel(ctx context.Context, cfg config.LLMConfig, schemaName string, schema map[string]any, messages []modelMessage) (string, error) {
	content, responsesErr := c.callResponsesInput(ctx, cfg, schemaName, schema, responseInputMessages(messages))
	if responsesErr == nil {
		return content, nil
	}

	reportProgress(ctx, "Trying Chat Completions compatibility fallback.")
	content, err := c.callChatCompletions(ctx, cfg, messages)
	if err == nil {
		reportProgress(ctx, "Model response received from Chat Completions fallback.")
		return content, nil
	}
	reportProgress(ctx, "Chat Completions fallback failed: "+shortError(err)+".")
	return "", modelRequestError([]string{responsesErr.Error(), "chat_completions: " + err.Error()})
}

func (c *Client) callResponsesInput(ctx context.Context, cfg config.LLMConfig, schemaName string, schema map[string]any, input []responseInput) (string, error) {
	attempts := responseFallbackAttempts(cfg)
	var failures []string
	for _, attempt := range attempts {
		reportProgress(ctx, "Calling model: "+describeResponseAttempt(attempt.cfg)+".")
		content, err := c.doResponses(ctx, attempt.cfg, schemaName, schema, input)
		if err == nil {
			reportProgress(ctx, "Model response received from Responses API.")
			return content, nil
		}
		failures = append(failures, attempt.label+": "+err.Error())
		reportProgress(ctx, "Model attempt failed: "+shortError(err)+".")
		if !shouldTryFallback(err) {
			return "", err
		}
	}
	return "", modelRequestError(failures)
}

func (c *Client) doResponses(ctx context.Context, cfg config.LLMConfig, schemaName string, schema map[string]any, input []responseInput) (string, error) {
	content, err := c.doResponsesRequest(ctx, cfg, schemaName, schema, input, true)
	if err == nil {
		return content, nil
	}
	if !isStreamingUnsupported(err) {
		return "", err
	}
	reportProgress(ctx, "Provider does not support Responses streaming; retrying without stream.")
	return c.doResponsesRequest(ctx, cfg, schemaName, schema, input, false)
}

func (c *Client) doResponsesRequest(ctx context.Context, cfg config.LLMConfig, schemaName string, schema map[string]any, input []responseInput, stream bool) (string, error) {
	endpoint := responsesEndpoint(cfg.BaseURL)
	reqBody := responsesRequest{
		Model:  cfg.Model,
		Input:  input,
		Stream: stream,
		Text: &responsesText{
			Format: &responsesTextFormat{
				Type:   "json_schema",
				Name:   schemaName,
				Strict: true,
				Schema: schema,
			},
		},
	}
	if cfg.ReasoningEffort != "" && cfg.ReasoningEffort != "none" {
		reqBody.Reasoning = &responsesReasoning{Effort: cfg.ReasoningEffort}
	}
	if cfg.EnableWebSearch {
		reqBody.Tools = []responsesTool{{Type: "web_search_preview"}}
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := c.doHTTPRequest(req, cfg.Timeout)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return "", err
		}
		return "", llmHTTPError{status: resp.StatusCode, body: string(body)}
	}
	if stream && isEventStream(resp.Header.Get("Content-Type")) {
		return readResponsesStream(ctx, resp.Body)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}

	var out responsesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", errors.New(out.Error.Message)
	}
	content := strings.TrimSpace(out.OutputText)
	if content == "" {
		content = extractResponsesText(out)
	}
	if content == "" {
		return "", errors.New("llm response did not contain output text")
	}
	return content, nil
}

func responseInputMessages(messages []modelMessage) []responseInput {
	input := make([]responseInput, 0, len(messages))
	for _, message := range messages {
		input = append(input, responseInput{
			Role: message.Role,
			Content: []responseContentPart{
				{Type: "input_text", Text: message.Content},
			},
		})
	}
	return input
}

func normalizeImageMimeType(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image/png":
		return "image/png", true
	case "image/jpeg", "image/jpg":
		return "image/jpeg", true
	case "image/webp":
		return "image/webp", true
	default:
		return "", false
	}
}

func imageDataURL(mimeType string, image []byte) string {
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(image)
}

func imageCADUserPrompt(req GenerateFromImageRequest) string {
	var parts []string
	parts = append(parts, "User request: "+req.Prompt)
	if strings.TrimSpace(req.FileName) != "" {
		parts = append(parts, "Uploaded file name: "+strings.TrimSpace(req.FileName))
	}
	parts = append(parts, "Task: inspect the image and generate one assembled Cascade Studio JavaScript CAD model.")
	parts = append(parts, "If it is a dimensioned drawing, extract the shown dimensions and use them exactly. If it is a photo or object image, infer a robust CAD approximation and list important assumptions.")
	return strings.Join(parts, "\n")
}

func extractResponsesText(out responsesResponse) string {
	var parts []string
	for _, item := range out.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Text != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func responsesEndpoint(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return strings.TrimSuffix(baseURL, "/chat/completions") + "/responses"
	}
	if strings.HasSuffix(baseURL, "/responses") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/responses"
	}
	return baseURL + "/v1/responses"
}

type responseFallbackAttempt struct {
	label string
	cfg   config.LLMConfig
}

func responseFallbackAttempts(cfg config.LLMConfig) []responseFallbackAttempt {
	attempts := []responseFallbackAttempt{{label: "responses_configured", cfg: cfg}}
	if cfg.EnableWebSearch {
		withoutSearch := cfg
		withoutSearch.EnableWebSearch = false
		attempts = append(attempts, responseFallbackAttempt{label: "responses_without_web_search", cfg: withoutSearch})

		if cfg.ReasoningEffort != "" && cfg.ReasoningEffort != "none" {
			withoutSearchOrReasoning := withoutSearch
			withoutSearchOrReasoning.ReasoningEffort = "none"
			attempts = append(attempts, responseFallbackAttempt{label: "responses_without_web_search_or_reasoning", cfg: withoutSearchOrReasoning})
		}
	} else if cfg.ReasoningEffort != "" && cfg.ReasoningEffort != "none" {
		withoutReasoning := cfg
		withoutReasoning.ReasoningEffort = "none"
		attempts = append(attempts, responseFallbackAttempt{label: "responses_without_reasoning", cfg: withoutReasoning})
	}
	return attempts
}

func describeResponseAttempt(cfg config.LLMConfig) string {
	features := []string{"Responses API"}
	if cfg.ReasoningEffort != "" && cfg.ReasoningEffort != "none" {
		features = append(features, "reasoning="+cfg.ReasoningEffort)
	}
	if cfg.EnableWebSearch {
		features = append(features, "web_search")
	}
	return strings.Join(features, ", ")
}

func (c *Client) callChatCompletions(ctx context.Context, cfg config.LLMConfig, messages []modelMessage) (string, error) {
	content, err := c.doChatCompletions(ctx, cfg, messages, true, true)
	if err != nil && isStreamingUnsupported(err) {
		reportProgress(ctx, "Provider does not support Chat Completions streaming; retrying without stream.")
		content, err = c.doChatCompletions(ctx, cfg, messages, true, false)
	}
	if err != nil && isResponseFormatUnsupported(err) {
		return c.doChatCompletions(ctx, cfg, messages, false, false)
	}
	return content, err
}

func (c *Client) doChatCompletions(ctx context.Context, cfg config.LLMConfig, messages []modelMessage, useJSONFormat bool, stream bool) (string, error) {
	reqBody := chatCompletionRequest{
		Model:       cfg.Model,
		Messages:    chatMessages(messages),
		Temperature: 0.1,
		Stream:      stream,
	}
	if useJSONFormat {
		reqBody.ResponseFormat = &chatResponseFormat{Type: "json_object"}
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsEndpoint(cfg.BaseURL), bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := c.doHTTPRequest(req, cfg.Timeout)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return "", err
		}
		return "", llmHTTPError{status: resp.StatusCode, body: string(body)}
	}
	if stream && isEventStream(resp.Header.Get("Content-Type")) {
		return readChatCompletionsStream(ctx, resp.Body)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}

	var out chatCompletionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", errors.New(out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("llm chat response contained no choices")
	}
	return out.Choices[0].Message.Content, nil
}

func chatMessages(messages []modelMessage) []chatMessage {
	out := make([]chatMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, chatMessage{Role: message.Role, Content: message.Content})
	}
	return out
}

func chatCompletionsEndpoint(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/responses") {
		return strings.TrimSuffix(baseURL, "/responses") + "/chat/completions"
	}
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/chat/completions"
	}
	return baseURL + "/v1/chat/completions"
}

func readResponsesStream(ctx context.Context, reader io.Reader) (string, error) {
	reportProgress(ctx, "Receiving streamed model response.")
	var out strings.Builder
	err := readSSEData(reader, func(data string) error {
		if data == "[DONE]" {
			return nil
		}
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta,omitempty"`
			Text  string `json:"text,omitempty"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil
		}
		if event.Error != nil && event.Error.Message != "" {
			return errors.New(event.Error.Message)
		}
		switch event.Type {
		case "response.output_text.delta":
			appendStreamDelta(ctx, &out, event.Delta)
		case "response.output_text.done":
			if out.Len() == 0 {
				appendStreamDelta(ctx, &out, event.Text)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(out.String())
	if content == "" {
		return "", errors.New("streaming response did not contain output text")
	}
	return content, nil
}

func readChatCompletionsStream(ctx context.Context, reader io.Reader) (string, error) {
	reportProgress(ctx, "Receiving streamed Chat Completions response.")
	var out strings.Builder
	err := readSSEData(reader, func(data string) error {
		if data == "[DONE]" {
			return nil
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content,omitempty"`
				} `json:"delta"`
				Message struct {
					Content string `json:"content,omitempty"`
				} `json:"message"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil
		}
		if event.Error != nil && event.Error.Message != "" {
			return errors.New(event.Error.Message)
		}
		for _, choice := range event.Choices {
			if choice.Delta.Content != "" {
				appendStreamDelta(ctx, &out, choice.Delta.Content)
			} else if choice.Message.Content != "" {
				appendStreamDelta(ctx, &out, choice.Message.Content)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(out.String())
	if content == "" {
		return "", errors.New("streaming chat response did not contain content")
	}
	return content, nil
}

func readSSEData(reader io.Reader, handle func(string) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		return handle(strings.TrimSpace(data))
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func appendStreamDelta(ctx context.Context, out *strings.Builder, delta string) {
	if delta == "" {
		return
	}
	out.WriteString(delta)
	reportProgress(ctx, "MODEL_DELTA "+delta)
}

func isEventStream(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

func isStreamingUnsupported(err error) bool {
	var httpErr llmHTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	if httpErr.status != http.StatusBadRequest &&
		httpErr.status != http.StatusNotFound &&
		httpErr.status != http.StatusUnprocessableEntity {
		return false
	}
	body := strings.ToLower(httpErr.body)
	return strings.Contains(body, "stream") ||
		strings.Contains(body, "event-stream") ||
		strings.Contains(body, "unsupported")
}

func shouldTryFallback(err error) bool {
	var httpErr llmHTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	switch httpErr.status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
		return true
	default:
		return false
	}
}

func isResponseFormatUnsupported(err error) bool {
	var httpErr llmHTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	if httpErr.status != http.StatusBadRequest && httpErr.status != http.StatusUnprocessableEntity {
		return false
	}
	body := strings.ToLower(httpErr.body)
	return strings.Contains(body, "response_format") ||
		strings.Contains(body, "json_object") ||
		strings.Contains(body, "unsupported")
}

func modelRequestError(failures []string) error {
	detail := strings.Join(failures, " | ")
	if len(detail) > 1600 {
		detail = detail[:1600] + "..."
	}
	return fmt.Errorf("llm request failed after fallback attempts. The configured provider may not support Responses API, web search, reasoning effort, or model name. Attempts: %s", detail)
}

func shortError(err error) string {
	message := strings.TrimSpace(err.Error())
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 220 {
		return message[:220] + "..."
	}
	return message
}

func decodeJSONContent(content string, target any) error {
	content = extractJSONObject(content)
	if content == "" {
		return errors.New("model response did not contain a JSON object")
	}
	return json.Unmarshal([]byte(content), target)
}

func generateCADJSONSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"code":        stringSchema(),
			"explanation": stringSchema(),
			"warnings":    arraySchema(stringSchema()),
		},
		[]string{"code", "explanation", "warnings"},
	)
}

func repairCADJSONSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"code":    stringSchema(),
			"changes": arraySchema(stringSchema()),
		},
		[]string{"code", "changes"},
	)
}

func refineCADJSONSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"code":    stringSchema(),
			"changes": arraySchema(stringSchema()),
		},
		[]string{"code", "changes"},
	)
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func arraySchema(items map[string]any) map[string]any {
	return map[string]any{
		"type":  "array",
		"items": items,
	}
}

func generateSystemPrompt() string {
	return `You generate CAD code for AI OpenCAD.
Return JSON only: {"code":"Cascade Studio JavaScript code","explanation":"short explanation","warnings":["optional warnings"]}.

Generate Cascade Studio JavaScript, not OpenSCAD.
When the user asks for a real-world object, use web search to verify practical dimensions, assembly layout, and mechanical details before writing code.
Briefly mention the key source-informed design assumptions in explanation, without URLs unless essential.
The code runs directly in Cascade Studio's browser worker with these functions in scope:
- Box(x, y, z, centered)
- Cylinder(radius, height, centered)
- Cone(radius1, radius2, height)
- Sphere(radius)
- Translate([x, y, z], shape)
- Rotate([axisX, axisY, axisZ], degrees, shape)
- Scale([x, y, z], shape)
- Union([shape1, shape2])
- Difference(mainBody, [tool1, tool2])
- Intersection([shape1, shape2])
- FilletEdges(shape, radius, edgeSelector)
- Edges(shape), Faces(shape)

Rules:
- Plain JavaScript only inside the code string. No Markdown fences.
- Output one complete self-contained script. Do not use import, export, await, async, fetch, window, document, self, require, module, or external libraries.
- Do not use OpenSCAD syntax such as cube(), cylinder(), difference() blocks, module, children(), or $fn.
- Do not use chain methods like shape.translate() or shape.rotate().
- Do not call sceneShapes.push(), do not assign to self.sceneShapes, and do not manually manage the scene.
- Use millimeters.
- Generate an assembled model: all parts must be positioned, aligned, and joined as a finished assembly, not scattered loose pieces.
- Prefer more detailed, manufacturable geometry when it remains robust: ribs, fillets/chamfers, mounting holes, bosses, slots, relief cuts, and realistic clearances.
- The final statement must create or return renderable geometry by calling Box, Sphere, Cylinder, Cone, Union, Difference, or Intersection.
- Make one final solid or a small set of solids remain renderable in the scene.
- For booleans, capture shapes in variables and call Difference(main, [cutters]) or Union([parts]).
- Difference and Union arguments must be valid shapes created earlier in the code; cutters must overlap the main body when subtracting.
- Keep geometry simple and robust for browser OpenCascade.`
}

func generateImageSystemPrompt() string {
	return `You generate CAD code for AI OpenCAD from an uploaded image.
Return JSON only: {"code":"Cascade Studio JavaScript code","explanation":"short explanation","warnings":["optional warnings"]}.

Generate Cascade Studio JavaScript, not OpenSCAD.
Carefully classify the image before modeling:
- If it is a technical drawing, blueprint, dimensioned sketch, CAD screenshot, or annotated diagram, extract all visible dimensions, hole positions, radii, angles, thicknesses, and symmetry constraints. Use those dimensions directly in millimeters when units are visible; if units are absent, state the assumed unit scale.
- If it is a real object photo, product photo, or concept image, infer the main solids, proportions, assembly relationships, ergonomic/mechanical features, and likely manufacturable dimensions. Use realistic approximate measurements and list important assumptions in warnings.
- If the image contains text labels or dimension callouts, use them as higher priority than visual proportions.
- If details are hidden or ambiguous, create a robust simplified CAD approximation instead of inventing fragile decorative details.

The code runs directly in Cascade Studio's browser worker with these functions in scope:
- Box(x, y, z, centered)
- Cylinder(radius, height, centered)
- Cone(radius1, radius2, height)
- Sphere(radius)
- Translate([x, y, z], shape)
- Rotate([axisX, axisY, axisZ], degrees, shape)
- Scale([x, y, z], shape)
- Union([shape1, shape2])
- Difference(mainBody, [tool1, tool2])
- Intersection([shape1, shape2])
- FilletEdges(shape, radius, edgeSelector)
- Edges(shape), Faces(shape)

Rules:
- Plain JavaScript only inside the code string. No Markdown fences.
- Output one complete self-contained script. Do not use import, export, await, async, fetch, window, document, self, require, module, or external libraries.
- Do not use OpenSCAD syntax such as cube(), cylinder(), difference() blocks, module, children(), or $fn.
- Do not use chain methods like shape.translate() or shape.rotate().
- Do not call sceneShapes.push(), do not assign to self.sceneShapes, and do not manually manage the scene.
- Use millimeters.
- Generate an assembled model: all parts must be positioned, aligned, and joined as a finished assembly, not scattered loose pieces.
- Prefer robust manufacturable geometry: ribs, fillets/chamfers, mounting holes, bosses, slots, relief cuts, and realistic clearances when supported by the image.
- The final statement must create or return renderable geometry by calling Box, Sphere, Cylinder, Cone, Union, Difference, or Intersection.
- Make one final solid or a small set of solids remain renderable in the scene.
- For booleans, capture shapes in variables and call Difference(main, [cutters]) or Union([parts]).
- Difference and Union arguments must be valid shapes created earlier in the code; cutters must overlap the main body when subtracting.
- Keep geometry simple and robust for browser OpenCascade.`
}

func repairSystemPrompt() string {
	return `You repair Cascade Studio JavaScript CAD code for AI OpenCAD.
Return JSON only: {"code":"repaired Cascade Studio JavaScript code","changes":["what changed"]}.

The repaired code must be plain Cascade Studio JavaScript, not OpenSCAD.
Use Box, Cylinder, Sphere, Translate, Rotate, Union, Difference, and related Cascade Studio functions directly.
Do not include Markdown fences, chain methods, OpenSCAD blocks, module, children(), $fn, import, export, await, async, fetch, window, document, self, require, sceneShapes.push(), or self.sceneShapes.
Preserve the user's design intent and make the code leave renderable shapes in sceneShapes.
The final statement must create or return renderable geometry by calling Box, Sphere, Cylinder, Cone, Union, Difference, or Intersection.
Keep the repaired model assembled: all parts must be positioned, aligned, and joined as a finished assembly, not scattered loose pieces.`
}

func refineSystemPrompt() string {
	return `You refine existing Cascade Studio JavaScript CAD code for AI OpenCAD.
Return JSON only: {"code":"updated Cascade Studio JavaScript code","changes":["what changed"]}.

You will receive the user's original prompt, the current CAD code, and a new modification instruction.
Use web search when the modification depends on real-world dimensions, standards, product fit, or mechanical references.
Modify the current model in-place conceptually: preserve the existing design, dimensions, and assembly unless the instruction asks to change them.
Make the smallest safe code changes needed to satisfy the instruction.

Rules:
- Output complete runnable Cascade Studio JavaScript code, not a patch or diff.
- Plain JavaScript only inside the code string. No Markdown fences.
- Output one complete self-contained script. Do not use import, export, await, async, fetch, window, document, self, require, module, or external libraries.
- Do not use OpenSCAD syntax such as cube(), cylinder(), difference() blocks, module, children(), or $fn.
- Do not use chain methods like shape.translate() or shape.rotate().
- Do not call sceneShapes.push(), do not assign to self.sceneShapes, and do not manually manage the scene.
- Keep the model assembled: all parts must be positioned, aligned, and joined as a finished assembly, not scattered loose pieces.
- The final statement must create or return renderable geometry by calling Box, Sphere, Cylinder, Cone, Union, Difference, or Intersection.
- Make one final solid or a small set of solids remain renderable in the scene.
- Keep geometry simple and robust for browser OpenCascade.`
}

var fencePattern = regexp.MustCompile("(?s)```(?:json|javascript|js|openscad|scad)?\\s*(.*?)\\s*```")

func cleanupCode(code string) string {
	code = strings.TrimSpace(code)
	matches := fencePattern.FindStringSubmatch(code)
	if len(matches) == 2 {
		code = strings.TrimSpace(matches[1])
	}
	return code
}

func extractJSONObject(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "{") && strings.HasSuffix(content, "}") {
		return content
	}
	matches := fencePattern.FindStringSubmatch(content)
	if len(matches) == 2 {
		content = strings.TrimSpace(matches[1])
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		return content[start : end+1]
	}
	return ""
}
