// Command external-agent-e2e is intentionally an external client: it imports
// no Runtime application package and exercises only the public HTTP contract.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type episodeStatus struct {
	Episode struct {
		EpisodeID string `json:"episode_id"`
		State     string `json:"state"`
	} `json:"episode"`
	ParentApproval *struct {
		ApprovalID string `json:"approval_id"`
	} `json:"parent_approval,omitempty"`
	Artifact any `json:"artifact,omitempty"`
}

func main() {
	set := flag.NewFlagSet("external-agent-e2e", flag.ExitOnError)
	server := set.String("server", envOr("COMMERCE_RUNTIME_URL", "http://127.0.0.1:8090"), "runtime HTTP URL")
	token := set.String("token", os.Getenv("COMMERCE_RUNTIME_API_TOKEN"), "runtime API token")
	file := set.String("file", "", "AcquireCapabilityRequest JSON file")
	requestID := set.String("request-id", "", "idempotency key override")
	autoApprove := set.Bool("auto-approve", false, "approve a returned parent request")
	decision := set.String("decision", "APPROVE", "parent decision when --auto-approve is set")
	timeout := set.Duration("timeout", 8*time.Minute, "end-to-end timeout")
	poll := set.Duration("poll", 250*time.Millisecond, "status polling interval")
	set.Parse(os.Args[1:])
	if strings.TrimSpace(*file) == "" {
		fail(errors.New("--file is required"))
	}
	body, err := os.ReadFile(*file)
	if err != nil {
		fail(err)
	}
	var identity struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(body, &identity); err != nil {
		fail(err)
	}
	key := strings.TrimSpace(*requestID)
	if key == "" {
		key = identity.RequestID
	}
	if key == "" {
		fail(errors.New("request_id is required in the acquire request or --request-id"))
	}
	base := strings.TrimRight(*server, "/")
	created, err := do(base+"/v1/episodes", http.MethodPost, *token, key, body)
	if err != nil {
		fail(err)
	}
	var initial episodeStatus
	if err := json.Unmarshal(created, &initial); err != nil || initial.Episode.EpisodeID == "" {
		fail(fmt.Errorf("invalid acquire response: %s", created))
	}
	final, err := pollEpisode(base, *token, initial.Episode.EpisodeID, *autoApprove, strings.ToUpper(*decision), *poll, *timeout)
	if err != nil {
		fail(err)
	}
	pretty, _ := json.MarshalIndent(final, "", "  ")
	fmt.Println(string(pretty))
}

func pollEpisode(base, token, episodeID string, autoApprove bool, decision string, interval, timeout time.Duration) (episodeStatus, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body, err := do(base+"/v1/episodes/"+episodeID, http.MethodGet, token, "", nil)
		if err != nil {
			return episodeStatus{}, err
		}
		var status episodeStatus
		if err := json.Unmarshal(body, &status); err != nil {
			return episodeStatus{}, err
		}
		switch status.Episode.State {
		case "FULFILLED":
			return status, nil
		case "FAILED", "BLOCKED", "ABORTED", "EXPIRED", "DISPUTED":
			return status, fmt.Errorf("episode reached terminal state %s", status.Episode.State)
		case "AWAITING_PARENT":
			if autoApprove && status.ParentApproval != nil {
				payload, _ := json.Marshal(map[string]string{"approval_id": status.ParentApproval.ApprovalID, "decision": decision, "actor_ref": "external-agent-e2e"})
				if _, err := do(base+"/v1/episodes/"+episodeID+"/parent-decisions", http.MethodPost, token, status.ParentApproval.ApprovalID, payload); err != nil {
					return episodeStatus{}, err
				}
				autoApprove = false
			}
		}
		time.Sleep(interval)
	}
	return episodeStatus{}, errors.New("external episode E2E timed out")
}

func do(url, method, token, idempotency string, body []byte) ([]byte, error) {
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	if idempotency != "" {
		request.Header.Set("Idempotency-Key", idempotency)
	}
	response, err := (&http.Client{Timeout: 95 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	value, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("runtime returned HTTP %d: %s", response.StatusCode, value)
	}
	return value, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
func fail(err error) { fmt.Fprintln(os.Stderr, "external-agent-e2e:", err); os.Exit(1) }
