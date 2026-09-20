// Package api exposes the narrow S10 external surface. HTTP and MCP share
// the same handlers and therefore the same authentication and runtime entry
// boundary.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
	runtime "github.com/stablepay/commerce-runtime/internal/runtime"
)

const maxRequestBytes = 1 << 20

type AuthConfig struct {
	Token         string
	AllowInsecure bool
}

type Server struct {
	service *application.Service
	store   repository.TransitionStore
	runner  *runtime.Runner
	ready   func(context.Context) error
	auth    AuthConfig
}

func NewServer(service *application.Service, store repository.TransitionStore, runner *runtime.Runner, auth AuthConfig, ready func(context.Context) error) *Server {
	return &Server{service: service, store: store, runner: runner, ready: ready, auth: auth}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		s.health(w)
		return
	}
	if r.URL.Path == "/readyz" {
		s.readyz(w, r)
		return
	}
	if r.URL.Path == "/mcp" {
		s.serveMCP(w, r)
		return
	}
	if r.URL.Path == "/v1/episodes" {
		s.createEpisode(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/episodes/") {
		s.episodeRoute(w, r)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "route not found")
}

func (s *Server) health(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "commerce-runtime"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.ready == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
		return
	}
	if err := s.ready(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "error": safeError(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (s *Server) authorize(r *http.Request) error {
	if s.auth.AllowInsecure && strings.TrimSpace(s.auth.Token) == "" {
		return nil
	}
	token := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if token == "" {
		token = strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.auth.Token)) != 1 {
		return errors.New("runtime API authentication failed")
	}
	return nil
}

func (s *Server) createEpisode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	if err := s.authorize(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var request contract.AcquireCapabilityRequest
	if err := decodeBody(w, r, &request); err != nil {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(request.RequestID)
	}
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key or request_id is required")
		return
	}
	if request.RequestID == "" {
		request.RequestID = idempotencyKey
	}
	if request.RequestID != idempotencyKey {
		writeError(w, http.StatusConflict, "idempotency_mismatch", "Idempotency-Key must match request_id")
		return
	}
	created, err := s.service.CreateEpisode(r.Context(), request)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if s.runner != nil {
		s.runner.Enqueue(context.Background(), created.Episode.EpisodeID)
	}
	status := http.StatusAccepted
	if created.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"episode": created.Episode, "replayed": created.Replayed})
}

func (s *Server) episodeRoute(w http.ResponseWriter, r *http.Request) {
	if err := s.authorize(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/episodes/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "episode id is required")
		return
	}
	episodeID := parts[0]
	if len(parts) == 2 && parts[1] == "events" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		events, err := s.service.ListEvents(r.Context(), episodeID)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"episode_id": episodeID, "events": events})
		return
	}
	if len(parts) == 2 && parts[1] == "parent-decisions" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		s.parentDecision(w, r, episodeID)
		return
	}
	if len(parts) != 1 || r.Method != http.MethodGet {
		writeError(w, http.StatusNotFound, "not_found", "episode route not found")
		return
	}
	s.status(w, r, episodeID)
}

type statusResponse struct {
	Episode        *episode.CommerceEpisode        `json:"episode"`
	Artifact       *invocation.DeliveryArtifact    `json:"artifact,omitempty"`
	Validation     *invocation.ValidationEvidence  `json:"validation,omitempty"`
	ParentApproval *recovery.ParentApprovalRequest `json:"parent_approval,omitempty"`
	ParentDecision *recovery.ParentDecisionFact    `json:"parent_decision,omitempty"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request, episodeID string) {
	value, err := s.service.GetEpisode(r.Context(), episodeID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response := statusResponse{Episode: value}
	if store, ok := s.store.(repository.S4Store); ok && len(value.DeliveryRefs) > 0 {
		response.Artifact, _ = store.GetDeliveryArtifact(r.Context(), value.DeliveryRefs[len(value.DeliveryRefs)-1])
	}
	if store, ok := s.store.(repository.S4Store); ok && len(value.ValidationEvidenceRefs) > 0 {
		response.Validation, _ = store.GetValidationEvidence(r.Context(), value.ValidationEvidenceRefs[len(value.ValidationEvidenceRefs)-1])
	}
	if store, ok := s.store.(parentApprovalLister); ok {
		if approvals, listErr := store.ListParentApprovalRequests(r.Context(), episodeID); listErr == nil && len(approvals) > 0 {
			response.ParentApproval = approvals[len(approvals)-1]
			if response.ParentApproval != nil {
				response.ParentDecision, _ = store.GetParentDecision(r.Context(), response.ParentApproval.ApprovalID)
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

type parentApprovalLister interface {
	repository.S5Store
	ListParentApprovalRequests(context.Context, string) ([]*recovery.ParentApprovalRequest, error)
}

type parentDecisionPayload struct {
	ApprovalID  string            `json:"approval_id"`
	Decision    recovery.Decision `json:"decision"`
	ActorRef    string            `json:"actor_ref"`
	FactsRef    string            `json:"facts_ref,omitempty"`
	PayloadHash string            `json:"payload_hash,omitempty"`
	OccurredAt  time.Time         `json:"occurred_at,omitempty"`
	TraceID     string            `json:"trace_id,omitempty"`
}

func (s *Server) parentDecision(w http.ResponseWriter, r *http.Request, episodeID string) {
	var payload parentDecisionPayload
	if err := decodeBody(w, r, &payload); err != nil {
		return
	}
	if strings.TrimSpace(payload.ApprovalID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_parent_decision", "approval_id is required")
		return
	}
	approvalStore, ok := s.store.(repository.S5Store)
	if !ok {
		writeError(w, http.StatusInternalServerError, "repository_unavailable", "parent approval store is unavailable")
		return
	}
	approval, err := approvalStore.GetParentApprovalRequest(r.Context(), payload.ApprovalID)
	if err != nil || approval.EpisodeID != episodeID {
		if err == nil {
			err = repository.ErrNotFound
		}
		writeDomainError(w, err)
		return
	}
	result, err := s.service.RecordParentDecision(r.Context(), application.ParentDecisionRequest{ApprovalID: payload.ApprovalID, Decision: payload.Decision, ActorRef: payload.ActorRef, FactsRef: payload.FactsRef, PayloadHash: payload.PayloadHash, OccurredAt: payload.OccurredAt, TraceID: payload.TraceID})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if s.runner != nil {
		s.runner.Enqueue(context.Background(), episodeID)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"episode": result.Episode, "approval_id": payload.ApprovalID, "replayed": result.Replayed})
}

func (s *Server) serveMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	if err := s.authorize(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var request mcpRequest
	if err := decodeBody(w, r, &request); err != nil {
		return
	}
	result := mcpResult{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		result.Result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "stablepay-commerce-runtime", "version": "s10"}}
	case "notifications/initialized":
		result.Result = map[string]any{}
	case "tools/list":
		result.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		var params mcpCallParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			result.Error = &mcpError{Code: -32602, Message: "invalid tools/call params"}
			break
		}
		result.Result = s.mcpCall(r.Context(), params)
	default:
		result.Error = &mcpError{Code: -32601, Message: "method not found"}
	}
	writeJSON(w, http.StatusOK, result)
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResult struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *mcpError `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type mcpCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func mcpTools() []map[string]any {
	return []map[string]any{
		{"name": "stablepay.acquire", "description": "Submit an AcquireCapabilityRequest to StablePay Runtime", "inputSchema": map[string]any{"type": "object"}},
		{"name": "stablepay.status", "description": "Read persisted episode progress and final artifact", "inputSchema": map[string]any{"type": "object", "required": []string{"episode_id"}, "properties": map[string]any{"episode_id": map[string]string{"type": "string"}}}},
		{"name": "stablepay.approve", "description": "Record a parent approval or denial and resume the episode", "inputSchema": map[string]any{"type": "object", "required": []string{"approval_id", "decision", "actor_ref"}, "properties": map[string]any{"approval_id": map[string]string{"type": "string"}, "decision": map[string]any{"type": "string", "enum": []string{"APPROVE", "DENY"}}, "actor_ref": map[string]string{"type": "string"}}}},
	}
}

func (s *Server) mcpCall(ctx context.Context, call mcpCallParams) map[string]any {
	callResult := func(value any, err error) map[string]any {
		if err != nil {
			return map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": safeError(err)}}}
		}
		payload, _ := json.Marshal(value)
		return map[string]any{"content": []map[string]string{{"type": "text", "text": string(payload)}}, "structuredContent": value}
	}
	switch call.Name {
	case "stablepay.acquire":
		var request contract.AcquireCapabilityRequest
		if err := json.Unmarshal(call.Arguments, &request); err != nil {
			return callResult(nil, err)
		}
		if request.RequestID == "" {
			return callResult(nil, errors.New("request_id is required"))
		}
		created, err := s.service.CreateEpisode(ctx, request)
		if err == nil && s.runner != nil {
			s.runner.Enqueue(context.Background(), created.Episode.EpisodeID)
		}
		return callResult(map[string]any{"episode": created.Episode, "replayed": created.Replayed}, err)
	case "stablepay.status":
		var args struct {
			EpisodeID string `json:"episode_id"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil || strings.TrimSpace(args.EpisodeID) == "" {
			return callResult(nil, errors.New("episode_id is required"))
		}
		value, err := s.statusValue(ctx, args.EpisodeID)
		return callResult(value, err)
	case "stablepay.approve":
		var payload parentDecisionPayload
		if err := json.Unmarshal(call.Arguments, &payload); err != nil {
			return callResult(nil, err)
		}
		approvalStore, ok := s.store.(repository.S5Store)
		if !ok {
			return callResult(nil, repository.ErrRepositoryUnavailable)
		}
		approval, err := approvalStore.GetParentApprovalRequest(ctx, payload.ApprovalID)
		if err != nil {
			return callResult(nil, err)
		}
		result, err := s.service.RecordParentDecision(ctx, application.ParentDecisionRequest{ApprovalID: payload.ApprovalID, Decision: payload.Decision, ActorRef: payload.ActorRef, FactsRef: payload.FactsRef, PayloadHash: payload.PayloadHash, OccurredAt: payload.OccurredAt, TraceID: payload.TraceID})
		if err == nil && s.runner != nil {
			s.runner.Enqueue(context.Background(), approval.EpisodeID)
		}
		return callResult(map[string]any{"episode": result.Episode, "approval_id": payload.ApprovalID, "replayed": result.Replayed}, err)
	default:
		return callResult(nil, fmt.Errorf("unknown MCP tool %q", call.Name))
	}
}

func (s *Server) statusValue(ctx context.Context, episodeID string) (statusResponse, error) {
	value, err := s.service.GetEpisode(ctx, episodeID)
	if err != nil {
		return statusResponse{}, err
	}
	response := statusResponse{Episode: value}
	if store, ok := s.store.(repository.S4Store); ok && len(value.DeliveryRefs) > 0 {
		response.Artifact, _ = store.GetDeliveryArtifact(ctx, value.DeliveryRefs[len(value.DeliveryRefs)-1])
	}
	if store, ok := s.store.(parentApprovalLister); ok {
		if approvals, listErr := store.ListParentApprovalRequests(ctx, episodeID); listErr == nil && len(approvals) > 0 {
			response.ParentApproval = approvals[len(approvals)-1]
			if response.ParentApproval != nil {
				response.ParentDecision, _ = store.GetParentDecision(ctx, response.ParentApproval.ApprovalID)
			}
		}
	}
	return response, nil
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return err
	}
	return nil
}

func writeDomainError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "invalid_request"
	switch {
	case errors.Is(err, repository.ErrNotFound), errors.Is(err, repository.ErrFactNotFound), errors.Is(err, repository.ErrPaymentIntentNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, repository.ErrRequestIDConflict), errors.Is(err, repository.ErrIdempotencyConflict), errors.Is(err, repository.ErrParentDecisionConflict), errors.Is(err, repository.ErrVersionConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, episode.ErrEpisodeExpired):
		status, code = http.StatusUnprocessableEntity, "episode_expired"
	case errors.Is(err, repository.ErrRepositoryUnavailable):
		status, code = http.StatusServiceUnavailable, "repository_unavailable"
	default:
		if strings.Contains(strings.ToLower(err.Error()), "not configured") || strings.Contains(strings.ToLower(err.Error()), "unavailable") {
			status, code = http.StatusServiceUnavailable, "dependency_unavailable"
		}
	}
	writeError(w, status, code, safeError(err))
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 512 {
		return message[:512]
	}
	return message
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
