package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/observability"
)

type Client struct {
	BaseURL      string
	Token        string
	HTTP         *http.Client
	PollInterval time.Duration
	PollTimeout  time.Duration
	InjectorURL  string
}

// Check probes the public health and readiness endpoints. It deliberately
// returns status codes instead of decoding server internals so the eval
// harness remains an external client.
func (c Client) Check(ctx context.Context) (healthStatus, readinessStatus int, err error) {
	c = c.normalized()
	healthStatus, _, err = c.do(ctx, http.MethodGet, c.BaseURL+"/healthz", "", nil)
	if err != nil {
		return 0, 0, err
	}
	readinessStatus, _, err = c.do(ctx, http.MethodGet, c.BaseURL+"/readyz", "", nil)
	return healthStatus, readinessStatus, err
}

func (c Client) normalized() Client {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 95 * time.Second}
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 500 * time.Millisecond
	}
	if c.PollTimeout <= 0 {
		c.PollTimeout = 5 * time.Minute
	}
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	return c
}

func (c Client) Run(ctx context.Context, scenarios []Scenario) ([]observability.EpisodeResult, error) {
	c = c.normalized()
	results := make([]observability.EpisodeResult, 0, len(scenarios))
	for _, scenario := range scenarios {
		if err := scenario.Validate(); err != nil {
			return nil, fmt.Errorf("case %s: %w", scenario.CaseID, err)
		}
		if scenario.ReplayResult != nil {
			copy := *scenario.ReplayResult
			copy.SchemaVersion = observability.SchemaVersion
			results = append(results, copy)
			continue
		}
		result := observability.EpisodeResult{SchemaVersion: observability.SchemaVersion, CaseID: scenario.CaseID, Seed: scenario.Seed, Mode: ModeLive, Ingress: scenario.Ingress, MemoryMode: scenario.MemoryMode, RecoveryProvider: scenario.RecoveryProvider, Path: scenario.Path, Failure: scenario.Failure}
		result.RequestID = requestID(scenario.Request)
		if result.RequestID == "" {
			result.CollectionError = "request_id is missing from live scenario"
			results = append(results, result)
			continue
		}
		result.SubmittedAt = time.Now().UTC()
		if scenario.Failure.Kind != "" && scenario.Failure.Kind != "none" {
			applied, evidence, err := c.prepareInjection(ctx, scenario)
			if err != nil {
				result.CollectionError = "fault injector: " + err.Error()
			} else {
				result.Failure.Applied = applied
				result.Failure.Evidence = evidence
			}
		}
		if result.CollectionError == "" {
			var episodeID string
			var err error
			switch scenario.Ingress {
			case IngressMCP:
				episodeID, err = c.submitMCP(ctx, scenario)
			default:
				// The CLI is itself an HTTP client. The harness uses the same
				// public request boundary and records the intended ingress label.
				episodeID, err = c.submitHTTP(ctx, scenario)
			}
			if err != nil {
				result.CollectionError = err.Error()
			} else {
				result.EpisodeID = episodeID
				trace, pollCount, pollErr := c.poll(ctx, episodeID, scenario.AutoApprove)
				result.PollCount = pollCount
				result.Trace = trace
				if pollErr != nil {
					result.CollectionError = pollErr.Error()
				}
			}
		}
		result.CompletedAt = time.Now().UTC()
		if !result.SubmittedAt.IsZero() && result.CompletedAt.After(result.SubmittedAt) {
			result.DurationMS = result.CompletedAt.Sub(result.SubmittedAt).Seconds() * 1000
		}
		results = append(results, result)
	}
	return results, nil
}

func (c Client) prepareInjection(ctx context.Context, scenario Scenario) (bool, string, error) {
	if strings.TrimSpace(c.InjectorURL) == "" {
		return false, "no external injector configured; fault request was not applied", nil
	}
	body, err := json.Marshal(map[string]any{"case_id": scenario.CaseID, "seed": scenario.Seed, "failure": scenario.Failure})
	if err != nil {
		return false, "", err
	}
	status, response, err := c.do(ctx, http.MethodPost, strings.TrimRight(c.InjectorURL, "/")+"/v1/injections", scenario.Failure.Kind, body)
	if err != nil {
		return false, "", err
	}
	if status < 200 || status >= 300 {
		return false, "", fmt.Errorf("injector returned HTTP %d: %s", status, safeBody(response))
	}
	var value struct {
		Applied  bool   `json:"applied"`
		Evidence string `json:"evidence"`
	}
	if err := json.Unmarshal(response, &value); err != nil {
		return false, "", err
	}
	return value.Applied, value.Evidence, nil
}

func (c Client) submitHTTP(ctx context.Context, scenario Scenario) (string, error) {
	status, body, err := c.do(ctx, http.MethodPost, c.BaseURL+"/v1/episodes", resultRequestID(scenario.Request), scenario.Request)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("runtime create returned HTTP %d: %s", status, safeBody(body))
	}
	var value struct {
		Episode struct {
			EpisodeID string `json:"episode_id"`
		} `json:"episode"`
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return "", err
	}
	if strings.TrimSpace(value.Episode.EpisodeID) == "" {
		return "", errors.New("runtime create response omitted episode_id")
	}
	return value.Episode.EpisodeID, nil
}

func (c Client) submitMCP(ctx context.Context, scenario Scenario) (string, error) {
	payload := map[string]any{"jsonrpc": "2.0", "id": scenario.CaseID, "method": "tools/call", "params": map[string]any{"name": "stablepay.acquire", "arguments": json.RawMessage(scenario.Request)}}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	status, response, err := c.do(ctx, http.MethodPost, c.BaseURL+"/mcp", resultRequestID(scenario.Request), body)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("runtime MCP returned HTTP %d: %s", status, safeBody(response))
	}
	var value struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			StructuredContent struct {
				Episode struct {
					EpisodeID string `json:"episode_id"`
				} `json:"episode"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &value); err != nil {
		return "", err
	}
	if value.Error != nil {
		return "", errors.New(value.Error.Message)
	}
	if strings.TrimSpace(value.Result.StructuredContent.Episode.EpisodeID) == "" {
		return "", errors.New("runtime MCP response omitted episode_id")
	}
	return value.Result.StructuredContent.Episode.EpisodeID, nil
}

func (c Client) poll(ctx context.Context, episodeID string, autoApprove bool) (observability.EpisodeTrace, int, error) {
	deadline := time.Now().Add(c.PollTimeout)
	polls := 0
	for time.Now().Before(deadline) {
		polls++
		status, body, err := c.do(ctx, http.MethodGet, c.BaseURL+"/v1/episodes/"+episodeID+"/observability", "", nil)
		if err != nil {
			return observability.EpisodeTrace{}, polls, err
		}
		if status == http.StatusNotFound {
			return observability.EpisodeTrace{}, polls, errors.New("episode disappeared during poll")
		}
		if status < 200 || status >= 300 {
			return observability.EpisodeTrace{}, polls, fmt.Errorf("runtime observability returned HTTP %d: %s", status, safeBody(body))
		}
		var traceValue observability.EpisodeTrace
		if err := json.Unmarshal(body, &traceValue); err != nil {
			return observability.EpisodeTrace{}, polls, err
		}
		if traceValue.Episode == nil {
			return observability.EpisodeTrace{}, polls, errors.New("observability response omitted episode")
		}
		if autoApprove && traceValue.Episode.State == episode.StateAwaitingParent && traceValue.ParentApproval != nil && traceValue.ParentDecision == nil {
			if err := c.approve(ctx, episodeID, traceValue.ParentApproval.ApprovalID); err != nil {
				return traceValue, polls, err
			}
		}
		if episode.IsTerminal(traceValue.Episode.State) {
			return traceValue, polls, nil
		}
		select {
		case <-ctx.Done():
			return traceValue, polls, ctx.Err()
		case <-time.After(c.PollInterval):
		}
	}
	return observability.EpisodeTrace{}, polls, fmt.Errorf("episode %s did not reach terminal state within %s", episodeID, c.PollTimeout)
}

func (c Client) approve(ctx context.Context, episodeID, approvalID string) error {
	body, _ := json.Marshal(map[string]string{"approval_id": approvalID, "decision": "APPROVE", "actor_ref": "s11-eval"})
	status, response, err := c.do(ctx, http.MethodPost, c.BaseURL+"/v1/episodes/"+episodeID+"/parent-decisions", approvalID, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("parent approval returned HTTP %d: %s", status, safeBody(response))
	}
	return nil
}

func (c Client) do(ctx context.Context, method, url, idempotency string, body []byte) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(c.Token) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}
	if strings.TrimSpace(idempotency) != "" {
		request.Header.Set("Idempotency-Key", idempotency)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	value, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	return response.StatusCode, value, err
}

func requestID(raw json.RawMessage) string {
	var value struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(raw, &value)
	return strings.TrimSpace(value.RequestID)
}

func resultRequestID(raw json.RawMessage) string { return requestID(raw) }

func safeBody(body []byte) string {
	value := strings.TrimSpace(string(body))
	if len(value) > 512 {
		return value[:512] + "..."
	}
	return value
}
