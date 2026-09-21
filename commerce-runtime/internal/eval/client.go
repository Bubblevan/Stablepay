package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/observability"
	"github.com/stablepay/commerce-runtime/internal/recovery"
)

type Client struct {
	BaseURL      string
	Token        string
	HTTP         *http.Client
	PollInterval time.Duration
	PollTimeout  time.Duration
	InjectorURL  string
	RuntimeCLI   string
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
			copy.Mode = ModeReplay
			copy.Failure = scenario.Failure
			copy.Failure.Applied = copy.Failure.EffectiveApplied()
			copy.Trace = redactTrace(copy.Trace)
			enrichResult(&copy, scenario)
			if len(copy.Grade.Assertions) == 0 {
				copy.Grade = observability.GradeEpisode(copy)
			}
			results = append(results, copy)
			continue
		}
		result := observability.EpisodeResult{SchemaVersion: observability.SchemaVersion, CaseID: scenario.CaseID, Seed: scenario.Seed, Mode: ModeLive, Ingress: scenario.Ingress, MemoryMode: scenario.MemoryMode, RecoveryProvider: scenario.RecoveryProvider, Path: scenario.Path, Failure: scenario.Failure}
		enrichResult(&result, scenario)
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
				result.Failure.Configured = applied
				result.Failure.Evidence = evidence
			}
		}
		if result.CollectionError == "" {
			var episodeID string
			var err error
			switch scenario.Ingress {
			case IngressMCP:
				episodeID, err = c.submitMCP(ctx, scenario)
			case IngressCLI:
				episodeID, err = c.submitCLI(ctx, scenario)
			default:
				episodeID, err = c.submitHTTP(ctx, scenario)
			}
			if err != nil {
				result.CollectionError = err.Error()
			} else {
				result.EpisodeID = episodeID
				trace, pollCount, pollErr := c.poll(ctx, episodeID, scenario.AutoApprove, scenario.Ingress)
				result.PollCount = pollCount
				result.Trace = redactTrace(trace)
				// For live episodes the persisted RuntimeVariant is authoritative.
				// Scenario labels remain expected configuration only.
				result.MemoryMode = result.Trace.RuntimeVariant.MemoryMode
				result.RecoveryProvider = result.Trace.RuntimeVariant.RecoveryProvider
				if pollErr != nil {
					result.CollectionError = pollErr.Error()
				}
			}
		}
		if result.CollectionError == "" && scenario.Failure.Kind != "" && scenario.Failure.Kind != "none" && strings.TrimSpace(c.InjectorURL) != "" {
			status, statusErr := c.injectionStatus(ctx)
			if statusErr != nil {
				result.CollectionError = "fault injector status: " + statusErr.Error()
			} else {
				result.Failure.Configured = status.Configured
				result.Failure.Triggered = status.InjectionCount > 0
				result.Failure.InjectionCount = status.InjectionCount
				result.Failure.RequestCount = status.RequestCount
				result.Failure.EligibleCount = status.EligibleCount
				result.Failure.LastInjectedAt = status.LastInjectedAt
				result.Failure.Evidence = status.Evidence
			}
		}
		if result.Failure.EffectiveApplied() {
			result.Failure.Applied = true
		} else {
			result.Failure.Applied = false
		}
		if result.CollectionError == "" {
			if err := result.Failure.ValidateObserved(); err != nil {
				result.CollectionError = "invalid fault observation: " + err.Error()
			}
		}
		if result.CollectionError == "" && hasVariantExpectation(scenario.ExpectedVariant) && !result.Trace.RuntimeVariant.Matches(scenario.ExpectedVariant) {
			result.CollectionError = "RUNTIME_VARIANT_MISMATCH"
		}
		result.Grade = observability.GradeEpisode(result)
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
		Configured bool   `json:"configured"`
		Evidence   string `json:"evidence"`
	}
	if err := json.Unmarshal(response, &value); err != nil {
		return false, "", err
	}
	return value.Configured, value.Evidence, nil
}

type FaultInjectionStatus struct {
	CaseID         string    `json:"case_id"`
	Seed           int64     `json:"seed"`
	Kind           string    `json:"kind"`
	Configured     bool      `json:"configured"`
	RequestCount   int       `json:"request_count"`
	EligibleCount  int       `json:"eligible_count"`
	InjectionCount int       `json:"injection_count"`
	LastInjectedAt time.Time `json:"last_injected_at"`
	Evidence       string    `json:"evidence"`
}

func (c Client) injectionStatus(ctx context.Context) (FaultInjectionStatus, error) {
	status, body, err := c.do(ctx, http.MethodGet, strings.TrimRight(c.InjectorURL, "/")+"/v1/injections/current", "", nil)
	if err != nil {
		return FaultInjectionStatus{}, err
	}
	if status < 200 || status >= 300 {
		return FaultInjectionStatus{}, fmt.Errorf("injector returned HTTP %d: %s", status, safeBody(body))
	}
	var value FaultInjectionStatus
	if err := json.Unmarshal(body, &value); err != nil {
		return FaultInjectionStatus{}, err
	}
	return value, nil
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

func (c Client) submitCLI(ctx context.Context, scenario Scenario) (string, error) {
	if strings.TrimSpace(c.RuntimeCLI) == "" {
		return "", errors.New("IngressCLI requires --runtime-cli; HTTP fallback is disabled")
	}
	file, err := os.CreateTemp(os.TempDir(), "stablepay-s11-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if _, err := file.Write(scenario.Request); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	defer os.Remove(path)
	args := []string{"acquire", "--server", c.BaseURL, "--file", path, "--idempotency-key", requestID(scenario.Request)}
	if strings.TrimSpace(c.Token) != "" {
		args = append(args, "--token", c.Token)
	}
	output, err := c.runCLI(ctx, args...)
	if err != nil {
		return "", err
	}
	var value struct {
		Episode struct {
			EpisodeID string `json:"episode_id"`
		} `json:"episode"`
	}
	if err := json.Unmarshal(output, &value); err != nil {
		return "", fmt.Errorf("decode runtime CLI acquire output: %w", err)
	}
	if value.Episode.EpisodeID == "" {
		return "", errors.New("runtime CLI acquire omitted episode_id")
	}
	return value.Episode.EpisodeID, nil
}

func (c Client) poll(ctx context.Context, episodeID string, autoApprove bool, ingress string) (observability.EpisodeTrace, int, error) {
	deadline := time.Now().Add(c.PollTimeout)
	polls := 0
	for time.Now().Before(deadline) {
		polls++
		var status int
		var body []byte
		var err error
		var traceValue observability.EpisodeTrace
		if ingress == IngressCLI {
			body, err = c.runCLI(ctx, "status", "--server", c.BaseURL, "--episode-id", episodeID)
			status = http.StatusOK
			if err == nil {
				var statusValue struct {
					Episode        *episode.CommerceEpisode        `json:"episode"`
					ParentApproval *recovery.ParentApprovalRequest `json:"parent_approval,omitempty"`
					ParentDecision *recovery.ParentDecisionFact    `json:"parent_decision,omitempty"`
				}
				if decodeErr := json.Unmarshal(body, &statusValue); decodeErr != nil {
					err = decodeErr
				} else {
					traceValue.Episode = statusValue.Episode
					traceValue.ParentApproval = statusValue.ParentApproval
					traceValue.ParentDecision = statusValue.ParentDecision
				}
			}
		} else {
			status, body, err = c.do(ctx, http.MethodGet, c.BaseURL+"/v1/episodes/"+episodeID+"/observability", "", nil)
		}
		if err != nil {
			return observability.EpisodeTrace{}, polls, err
		}
		if status == http.StatusNotFound {
			return observability.EpisodeTrace{}, polls, errors.New("episode disappeared during poll")
		}
		if status < 200 || status >= 300 {
			return observability.EpisodeTrace{}, polls, fmt.Errorf("runtime observability returned HTTP %d: %s", status, safeBody(body))
		}
		if ingress != IngressCLI {
			if err := json.Unmarshal(body, &traceValue); err != nil {
				return observability.EpisodeTrace{}, polls, err
			}
		}
		if traceValue.Episode == nil {
			return observability.EpisodeTrace{}, polls, errors.New("observability response omitted episode")
		}
		if autoApprove && traceValue.Episode.State == episode.StateAwaitingParent && traceValue.ParentApproval != nil && traceValue.ParentDecision == nil {
			if err := c.approve(ctx, episodeID, traceValue.ParentApproval.ApprovalID, ingress); err != nil {
				return traceValue, polls, err
			}
		}
		if episode.IsTerminal(traceValue.Episode.State) {
			if ingress == IngressCLI {
				observabilityStatus, observabilityBody, observabilityErr := c.do(ctx, http.MethodGet, c.BaseURL+"/v1/episodes/"+episodeID+"/observability", "", nil)
				if observabilityErr != nil {
					return traceValue, polls, observabilityErr
				}
				if observabilityStatus < 200 || observabilityStatus >= 300 {
					return traceValue, polls, fmt.Errorf("runtime observability returned HTTP %d: %s", observabilityStatus, safeBody(observabilityBody))
				}
				if err := json.Unmarshal(observabilityBody, &traceValue); err != nil {
					return traceValue, polls, err
				}
			}
			return traceValue, polls, nil
		}
		// A persisted execution error is an externally observable benchmark
		// outcome even when the episode is non-terminal. Preserve its trace for
		// grading instead of turning a guard rejection into a collection error.
		if traceValue.Execution != nil && strings.EqualFold(string(traceValue.Execution.Status), "ERROR") {
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

func (c Client) approve(ctx context.Context, episodeID, approvalID, ingress string) error {
	if ingress == IngressCLI {
		args := []string{"approve", "--server", c.BaseURL, "--episode-id", episodeID, "--approval-id", approvalID, "--decision", "APPROVE", "--actor-ref", "s11-eval"}
		if strings.TrimSpace(c.Token) != "" {
			args = append(args, "--token", c.Token)
		}
		_, err := c.runCLI(ctx, args...)
		return err
	}
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

func (c Client) runCLI(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, c.RuntimeCLI, args...)
	output, err := command.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("runtime CLI failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	return output, nil
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

func enrichResult(result *observability.EpisodeResult, scenario Scenario) {
	result.CaseID = scenario.CaseID
	result.Seed = scenario.Seed
	result.Ingress = scenario.Ingress
	result.MemoryMode = scenario.MemoryMode
	result.RecoveryProvider = scenario.RecoveryProvider
	result.Path = scenario.Path
	result.Suite = scenario.Suite
	if result.Suite == "" {
		result.Suite = "benchmark"
	}
	result.Environment = scenario.Environment
	if result.Environment == "" {
		if scenario.ReplayResult != nil {
			result.Environment = "replay"
		} else {
			result.Environment = "live_local"
		}
	}
	result.TaskID = scenario.TaskID
	if result.TaskID == "" {
		result.TaskID = scenario.CaseID
	}
	result.TrialIndex = scenario.TrialIndex
	result.ExpectedTerminal = scenario.ExpectedTerminal
	result.ExpectedVariant = scenario.ExpectedVariant
	result.ExpectedPaymentIntents = scenario.ExpectedPaymentIntents
	result.ExpectedSettlementCount = scenario.ExpectedSettlementCount
	result.ExpectedEntitlementTxID = scenario.ExpectedEntitlementTxID
	result.RequirePayment = scenario.RequirePayment
	result.RequireRecovery = scenario.RequireRecovery
	result.MustNotCreateSecondPayment = scenario.MustNotCreateSecondPayment
}

func hasVariantExpectation(value observability.RuntimeVariant) bool {
	return strings.TrimSpace(value.RuntimeVersion) != "" || strings.TrimSpace(value.MemoryMode) != "" || strings.TrimSpace(value.RecoveryProvider) != "" || strings.TrimSpace(value.LLMProvider) != "" || strings.TrimSpace(value.ModelRef) != "" || strings.TrimSpace(value.ConfigHash) != ""
}

func redactTrace(value observability.EpisodeTrace) observability.EpisodeTrace {
	if value.Artifact == nil {
		return value
	}
	artifact := *value.Artifact
	artifact.Body = nil
	artifact.ArtifactBodyIncluded = false
	value.Artifact = &artifact
	return value
}
