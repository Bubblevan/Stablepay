// Command s11-fault-proxy is an opt-in HTTP fault injector for S11. It is a
// separate process: the Commerce Runtime has no test-only fault switches.
// Point only one dependency (merchant, LLM, or verification) at this proxy.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/stablepay/commerce-runtime/internal/eval"
	"github.com/stablepay/commerce-runtime/internal/observability"
)

type proxy struct {
	mu       sync.Mutex
	upstream *httputil.ReverseProxy
	profile  observability.FailureInjection
	seed     int64
	count    int
}

func main() {
	listen := flag.String("listen", ":8790", "proxy listen address")
	upstreamValue := flag.String("upstream", "", "HTTP upstream URL")
	flag.Parse()
	if strings.TrimSpace(*upstreamValue) == "" {
		log.Fatal("--upstream is required")
	}
	upstream, err := url.Parse(*upstreamValue)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		log.Fatalf("invalid --upstream: %v", err)
	}
	value := httputil.NewSingleHostReverseProxy(upstream)
	value.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "fault proxy upstream unavailable", http.StatusBadGateway)
	}
	server := &http.Server{Addr: *listen, Handler: &proxy{upstream: value}, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("s11 fault proxy listening on %s -> %s", *listen, upstream.Redacted())
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "s11-fault-proxy"})
		return
	}
	if r.URL.Path == "/v1/injections" && r.Method == http.MethodPost {
		p.configure(w, r)
		return
	}
	if failure := p.nextFailure(r); failure != "" {
		p.inject(w, r, failure)
		return
	}
	p.upstream.ServeHTTP(w, r)
}

func (p *proxy) configure(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"applied": false, "evidence": err.Error()})
		return
	}
	var request struct {
		CaseID  string                         `json:"case_id"`
		Seed    int64                          `json:"seed"`
		Failure observability.FailureInjection `json:"failure"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"applied": false, "evidence": "invalid injection request"})
		return
	}
	if request.Failure.Kind == "payment_transient" || request.Failure.Kind == "payment_permanent" {
		writeJSON(w, http.StatusOK, map[string]any{"applied": false, "evidence": "payment requires a Kitex-aware external injector; HTTP proxy did not apply it"})
		return
	}
	if request.Failure.Kind == "crash_restart" {
		writeJSON(w, http.StatusOK, map[string]any{"applied": false, "evidence": "crash/restart requires an external process supervisor; HTTP proxy did not terminate Runtime"})
		return
	}
	p.mu.Lock()
	p.profile = request.Failure
	p.seed = request.Seed
	p.count = 0
	p.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"applied": true, "evidence": fmt.Sprintf("http fault profile %s installed for case %s", request.Failure.Kind, request.CaseID)})
}

func (p *proxy) nextFailure(r *http.Request) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.count++
	profile := p.profile
	if profile.Kind == "" || profile.Kind == "none" {
		return ""
	}
	// The first merchant call is the x402 challenge. Delivery-invalid must
	// preserve that challenge and corrupt the subsequent paid delivery.
	if profile.Kind == "delivery_invalid" && p.count == 1 {
		return ""
	}
	if profile.RatePercent > 0 && !eval.InjectAt(p.seed, profile.Kind, p.count, profile.RatePercent) {
		return ""
	}
	if profile.Kind == "merchant_transient" || profile.Kind == "llm_malformed" || profile.Kind == "llm_timeout" || profile.Kind == "verification_mismatch" || profile.Kind == "delivery_invalid" {
		if profile.Repeat > 0 && p.count > profile.Repeat {
			return ""
		}
	}
	if profile.Kind == "merchant_permanent" || profile.Kind == "payment_permanent" {
		return profile.Kind
	}
	if profile.Kind == "crash_restart" {
		return "crash_restart"
	}
	_ = r
	return profile.Kind
}

func (p *proxy) inject(w http.ResponseWriter, r *http.Request, kind string) {
	switch kind {
	case "llm_timeout":
		timer := time.NewTimer(60 * time.Second)
		select {
		case <-r.Context().Done():
			timer.Stop()
		case <-timer.C:
		}
		return
	case "llm_malformed":
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "not-json"}}}})
	case "delivery_invalid":
		writeJSON(w, http.StatusOK, map[string]any{"unexpected": true})
	case "verification_mismatch":
		writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ok", "data": map[string]any{"purchased": true, "tx_id": "s11-injected-mismatch", "tx_hash": "s11-injected-mismatch"}})
	case "crash_restart":
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "s11 crash/restart boundary requested; restart the Runtime process before resuming"})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "s11 injected transient/permanent failure", "kind": kind})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
