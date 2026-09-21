package eval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/observability"
)

func TestExternalClientUsesPublicHTTPMCPAndRuntimeChecks(t *testing.T) {
	var seenAuth, seenIdempotency []string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/healthz" || request.URL.Path == "/readyz" {
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"status":"ok"}`))
			return
		}
		if request.Header.Get("Authorization") != "Bearer eval-token" {
			http.Error(writer, "missing auth", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/v1/episodes":
			seenAuth = append(seenAuth, request.Header.Get("Authorization"))
			seenIdempotency = append(seenIdempotency, request.Header.Get("Idempotency-Key"))
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"episode":{"episode_id":"ce-eval-http"}}`))
		case "/mcp":
			seenAuth = append(seenAuth, request.Header.Get("Authorization"))
			seenIdempotency = append(seenIdempotency, request.Header.Get("Idempotency-Key"))
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":"eval-mcp","result":{"structuredContent":{"episode":{"episode_id":"ce-eval-mcp"}}}}`))
		case "/v1/episodes/ce-eval-http/observability", "/v1/episodes/ce-eval-mcp/observability":
			writer.Header().Set("Content-Type", "application/json")
			encoded, _ := json.Marshal(observability.EpisodeTrace{
				SchemaVersion: observability.SchemaVersion,
				Source:        "runtime_api",
				Episode:       &episode.CommerceEpisode{EpisodeID: "ce-eval", State: episode.StateFulfilled},
				Events:        []*episode.EpisodeEvent{},
			})
			_, _ = writer.Write(encoded)
		default:
			http.NotFound(writer, request)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := Client{BaseURL: server.URL, Token: "eval-token", PollInterval: time.Millisecond, PollTimeout: time.Second}

	health, ready, err := client.Check(context.Background())
	if err != nil || health != http.StatusOK || ready != http.StatusOK {
		t.Fatalf("public health/readiness check failed: health=%d ready=%d err=%v", health, ready, err)
	}
	request := json.RawMessage(`{"request_id":"eval-http","parent_session_id":"s11","requester_did":"did:agent:test","acquisition_goal":{"task_type":"test","description":"external eval"}}`)
	results, err := client.Run(context.Background(), []Scenario{
		{CaseID: "http", Seed: 1, Ingress: IngressHTTP, MemoryMode: "off", RecoveryProvider: "rule", Path: "happy", Request: request},
		{CaseID: "mcp", Seed: 2, Ingress: IngressMCP, MemoryMode: "off", RecoveryProvider: "rule", Path: "happy", Request: json.RawMessage(`{"request_id":"eval-mcp"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].EpisodeID != "ce-eval-http" || results[1].EpisodeID != "ce-eval-mcp" {
		t.Fatalf("unexpected external results: %#v", results)
	}
	for _, value := range results {
		if value.Trace.Episode == nil || value.Trace.Episode.State != episode.StateFulfilled {
			t.Fatalf("external result was not terminal: %#v", value.Trace)
		}
	}
	if strings.Join(seenAuth, ",") != "Bearer eval-token,Bearer eval-token" {
		t.Fatalf("auth did not reach public ingress: %#v", seenAuth)
	}
	if strings.Join(seenIdempotency, ",") != "eval-http,eval-mcp" {
		t.Fatalf("idempotency did not reach public ingress: %#v", seenIdempotency)
	}
}
