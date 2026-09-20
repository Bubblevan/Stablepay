package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// OpenAICompatibleClient is the first real network implementation. The
// application depends only on LLMClient, so replacing this transport does not
// change domain or runtime authority.
type OpenAICompatibleClient struct {
	endpoint string
	apiKey   string
	model    string
	http     HTTPDoer
}

func NewOpenAICompatibleClient(baseURL, apiKey, model string, client HTTPDoer) (*OpenAICompatibleClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(model) == "" {
		return nil, ErrLLMUnavailable
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("%w: invalid LLM base URL", ErrLLMUnavailable)
	}
	endpoint := baseURL
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		if strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/chat/completions"
		} else {
			endpoint += "/v1/chat/completions"
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	return &OpenAICompatibleClient{endpoint: endpoint, apiKey: apiKey, model: model, http: client}, nil
}

func NewConfiguredClient(provider, baseURL, apiKey, model string, client HTTPDoer) (LLMClient, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "deepseek", "openai", "openai-compatible", "compatible", "http":
		return NewOpenAICompatibleClient(baseURL, apiKey, model, client)
	default:
		return nil, fmt.Errorf("%w: unsupported LLM_PROVIDER", ErrLLMUnavailable)
	}
}

func NewProviderFromConfig(provider, baseURL, apiKey, model string, options ...ProviderOption) (*LLMDecisionProvider, error) {
	client, err := NewConfiguredClient(provider, baseURL, apiKey, model, nil)
	if err != nil {
		return nil, err
	}
	options = append(options, WithProviderName(provider), WithModelRef(model))
	return NewLLMDecisionProvider(client, options...), nil
}

type compatibleRequest struct {
	Model          string              `json:"model"`
	Messages       []compatibleMessage `json:"messages"`
	Temperature    float64             `json:"temperature"`
	ResponseFormat map[string]string   `json:"response_format"`
}

type compatibleMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type compatibleResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		InputTokens      int `json:"input_tokens"`
		OutputTokens     int `json:"output_tokens"`
	} `json:"usage"`
}

func (c *OpenAICompatibleClient) GenerateDecision(ctx context.Context, request LLMDecisionRequest) (LLMDecisionResponse, error) {
	if c == nil || c.http == nil {
		return LLMDecisionResponse{}, ErrLLMUnavailable
	}
	body, err := json.Marshal(compatibleRequest{Model: c.model, Messages: []compatibleMessage{{Role: "system", Content: request.Prompt.System}, {Role: "user", Content: request.Prompt.User}}, Temperature: 0, ResponseFormat: map[string]string{"type": "json_object"}})
	if err != nil {
		return LLMDecisionResponse{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return LLMDecisionResponse{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if request.ContextHash != "" {
		httpRequest.Header.Set("X-StablePay-Context-Hash", request.ContextHash)
	}
	response, err := c.http.Do(httpRequest)
	if err != nil {
		return LLMDecisionResponse{Provider: "openai-compatible", ModelRef: c.model, ResponseReceivedAt: time.Now().UTC()}, err
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	received := time.Now().UTC()
	if readErr != nil {
		return LLMDecisionResponse{Provider: "openai-compatible", ModelRef: c.model, ResponseReceivedAt: received}, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return LLMDecisionResponse{Provider: "openai-compatible", ModelRef: c.model, RawJSON: responseBody, ResponseReceivedAt: received}, fmt.Errorf("LLM endpoint returned HTTP %d", response.StatusCode)
	}
	var decoded compatibleResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return LLMDecisionResponse{Provider: "openai-compatible", ModelRef: c.model, RawJSON: responseBody, ResponseReceivedAt: received}, err
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return LLMDecisionResponse{Provider: "openai-compatible", ModelRef: c.model, RawJSON: responseBody, ResponseReceivedAt: received}, errors.New("LLM response did not include a decision message")
	}
	return LLMDecisionResponse{Provider: "openai-compatible", ModelRef: firstNonEmpty(decoded.Model, c.model), RawJSON: []byte(decoded.Choices[0].Message.Content), ResponseReceivedAt: received, Usage: LLMUsage{InputTokens: firstNonZero(decoded.Usage.InputTokens, decoded.Usage.PromptTokens), OutputTokens: firstNonZero(decoded.Usage.OutputTokens, decoded.Usage.CompletionTokens)}}, nil
}

func firstNonEmpty(left, right string) string {
	if strings.TrimSpace(left) != "" {
		return left
	}
	return right
}

func firstNonZero(left, right int) int {
	if left != 0 {
		return left
	}
	return right
}
