package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/workflow"
)

func (s *Server) workflowDefinitions(w http.ResponseWriter, r *http.Request) {
	if s.workflow == nil {
		writeError(w, http.StatusServiceUnavailable, "workflow_unavailable", "workflow runtime is unavailable")
		return
	}
	if err := s.authorize(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	var definition workflow.WorkflowDefinition
	if err := decodeBody(w, r, &definition); err != nil {
		return
	}
	value, err := s.workflow.RegisterDefinition(r.Context(), definition)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"definition": value})
}

func (s *Server) workflowDefinitionRoute(w http.ResponseWriter, r *http.Request) {
	if s.workflow == nil {
		writeError(w, http.StatusServiceUnavailable, "workflow_unavailable", "workflow runtime is unavailable")
		return
	}
	if err := s.authorize(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "invalid_workflow_identifier", "workflow paths cannot contain a query string")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/workflows/"), "/"), "/")
	if len(parts) != 2 || r.Method != http.MethodGet {
		writeError(w, http.StatusNotFound, "not_found", "workflow definition route not found")
		return
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_workflow_id", err.Error())
		return
	}
	version, err := url.PathUnescape(parts[1])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_workflow_version", err.Error())
		return
	}
	if err := workflow.ValidateIdentifier(id); err != nil {
		writeDomainError(w, err)
		return
	}
	if err := workflow.ValidateIdentifier(version); err != nil {
		writeDomainError(w, err)
		return
	}
	value, err := s.workflow.GetDefinition(r.Context(), id, version)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"definition": value})
}

func (s *Server) workflowRuns(w http.ResponseWriter, r *http.Request) {
	if s.workflow == nil {
		writeError(w, http.StatusServiceUnavailable, "workflow_unavailable", "workflow runtime is unavailable")
		return
	}
	if err := s.authorize(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	var request workflow.WorkflowRunRequest
	if err := decodeBody(w, r, &request); err != nil {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		key = strings.TrimSpace(request.RequestID)
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key or request_id is required")
		return
	}
	if request.RequestID == "" {
		request.RequestID = key
	}
	if request.RequestID != key {
		writeError(w, http.StatusConflict, "idempotency_mismatch", "Idempotency-Key must match request_id")
		return
	}
	created, err := s.workflow.CreateRun(r.Context(), request)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	s.workflow.Enqueue(r.Context(), created.Run.WorkflowRunID)
	status := http.StatusAccepted
	if created.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"workflow_run": created.Run, "replayed": created.Replayed})
}

func (s *Server) workflowRunRoute(w http.ResponseWriter, r *http.Request) {
	if s.workflow == nil {
		writeError(w, http.StatusServiceUnavailable, "workflow_unavailable", "workflow runtime is unavailable")
		return
	}
	if err := s.authorize(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "invalid_workflow_identifier", "workflow paths cannot contain a query string")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/workflow-runs/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "workflow run id is required")
		return
	}
	runID := parts[0]
	if err := workflow.ValidateIdentifier(runID); err != nil {
		writeDomainError(w, err)
		return
	}
	if len(parts) == 2 && parts[1] == "artifact" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		value, err := s.workflow.GetFinalArtifact(r.Context(), runID)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"artifact": struct {
			Body        []byte `json:"body"`
			ContentType string `json:"content_type"`
			PayloadHash string `json:"payload_hash"`
		}{Body: value.Body, ContentType: value.ContentType, PayloadHash: value.PayloadHash}})
		return
	}
	if len(parts) == 2 && parts[1] == "events" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		events, err := s.workflow.ListEvents(r.Context(), runID)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workflow_run_id": runID, "events": events})
		return
	}
	if len(parts) == 2 && parts[1] == "observability" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
			return
		}
		value, err := s.workflow.GetObservability(r.Context(), runID)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	if len(parts) == 2 && parts[1] == "parent-decisions" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
			return
		}
		var payload parentDecisionPayload
		if err := decodeBody(w, r, &payload); err != nil {
			return
		}
		if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" && key != payload.ApprovalID {
			writeError(w, http.StatusConflict, "idempotency_mismatch", "Idempotency-Key must match approval_id")
			return
		}
		result, replayed, err := s.workflow.RecordParentDecision(r.Context(), runID, application.ParentDecisionRequest{ApprovalID: payload.ApprovalID, Decision: payload.Decision, ActorRef: payload.ActorRef, FactsRef: payload.FactsRef, PayloadHash: payload.PayloadHash, OccurredAt: payload.OccurredAt, TraceID: payload.TraceID})
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"workflow_run_id": runID, "episode": result, "replayed": replayed})
		return
	}
	if len(parts) != 1 || r.Method != http.MethodGet {
		writeError(w, http.StatusNotFound, "not_found", "workflow run route not found")
		return
	}
	status, err := s.workflow.GetStatus(r.Context(), runID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func workflowApproveArguments(arguments map[string]any) (string, application.ParentDecisionRequest, error) {
	runID, _ := arguments["workflow_run_id"].(string)
	payload := application.ParentDecisionRequest{ApprovalID: stringArgument(arguments, "approval_id"), ActorRef: stringArgument(arguments, "actor_ref"), FactsRef: stringArgument(arguments, "facts_ref"), PayloadHash: stringArgument(arguments, "payload_hash"), TraceID: stringArgument(arguments, "trace_id")}
	payload.Decision = recovery.Decision(strings.ToUpper(stringArgument(arguments, "decision")))
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(payload.ApprovalID) == "" || strings.TrimSpace(payload.ActorRef) == "" {
		return "", payload, workflow.ErrInvalidWorkflowRun
	}
	return runID, payload, nil
}

func stringArgument(arguments map[string]any, key string) string {
	value, _ := arguments[key].(string)
	return strings.TrimSpace(value)
}
