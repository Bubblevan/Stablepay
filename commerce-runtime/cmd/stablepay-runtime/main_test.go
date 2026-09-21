package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkflowStatusCLIUsesAuthenticatedPublicAPI(t *testing.T) {
	var gotPath, gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"workflow_run":{"state":"FULFILLED"}}`)
	}))
	defer server.Close()

	if err := run([]string{"workflow", "status", "--server", server.URL, "--token", "cli-token", "--workflow-run-id", "wr:cli-test"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/workflow-runs/wr:cli-test" || gotAuthorization != "Bearer cli-token" {
		t.Fatalf("CLI did not use the authenticated public workflow API: path=%q authorization=%q", gotPath, gotAuthorization)
	}
}
