package procurementmvp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/procurement"
)

type HTTPConfig struct {
	AllowedOrigins []string
	SecureCookies  bool
	CampusReview   bool
}

func Handler(service *Service, cfg HTTPConfig) http.Handler {
	gate := &requestGate{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/healthz" && r.Method == "GET" {
			reply(w, 200, map[string]string{"service": "commerce-runtime-procurement", "status": "ok"})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/mvp/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" {
			if r.Header.Get("X-Requested-With") != "StablePay" {
				replyError(w, errors.New("缺少请求来源标记"), 403)
				return
			}
			origin := r.Header.Get("Origin")
			if origin != "" {
				allowed := false
				for _, candidate := range cfg.AllowedOrigins {
					if origin == candidate {
						allowed = true
					}
				}
				if !allowed {
					replyError(w, errors.New("请求来源不受支持"), 403)
					return
				}
			}
		}
		cookie, err := r.Cookie("stablepay_mvp")
		session := ""
		if err == nil {
			decoded, decodeErr := hex.DecodeString(cookie.Value)
			if decodeErr == nil && len(decoded) == 32 {
				session = cookie.Value
			}
		}
		if session == "" {
			session = id() + id()
			http.SetCookie(w, &http.Cookie{Name: "stablepay_mvp", Value: session, Path: "/", HttpOnly: true, Secure: cfg.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 24 * 3600})
		}
		hash := sha256.Sum256([]byte(session))
		owner := hex.EncodeToString(hash[:])
		if r.Method != "GET" && !gate.allow(owner, time.Now()) {
			w.Header().Set("Retry-After", "60")
			replyError(w, errors.New("请求过于频繁，请一分钟后重试"), 429)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/mvp/")
		switch {
		case path == "status" && r.Method == "GET":
			mode, err := service.PaymentMode(r.Context())
			if err != nil {
				replyError(w, err, 503)
				return
			}
			reply(w, 200, map[string]any{"payment_mode": mode, "planner_configured": service.Planner.Client != nil, "live_payments_enabled": false, "review_profile": cfg.CampusReview})
			return
		case path == "catalog" && r.Method == "GET":
			reply(w, 200, Products())
			return
		case path == "plans" && r.Method == "POST":
			var body struct {
				Prompt        string            `json:"prompt"`
				ServiceBudget string            `json:"service_budget"`
				Input         procurement.Input `json:"input"`
			}
			if !decode(w, r, &body) {
				return
			}
			plan, err := service.CreatePlan(r.Context(), owner, body.Prompt, body.ServiceBudget, body.Input)
			respond(w, plan, err)
			return
		case path == "quotes" && r.Method == "POST":
			var body struct {
				PlanID    string `json:"plan_id"`
				ProductID string `json:"product_id"`
			}
			if !decode(w, r, &body) {
				return
			}
			quote, err := service.CreateQuote(r.Context(), owner, body.PlanID, body.ProductID)
			respond(w, quote, err)
			return
		case path == "purchases" && r.Method == "POST":
			var body struct {
				QuoteID   string `json:"quote_id"`
				QuoteHash string `json:"quote_hash"`
				Accepted  bool   `json:"accepted"`
			}
			if !decode(w, r, &body) {
				return
			}
			if !body.Accepted {
				replyError(w, errors.New("需要你确认商品、商户和报价金额"), 400)
				return
			}
			purchase, err := service.Approve(r.Context(), owner, body.QuoteID, body.QuoteHash, r.Header.Get("Idempotency-Key"))
			respond(w, purchase, err)
			return
		case path == "purchases" && r.Method == "GET":
			purchases, err := service.List(owner)
			respond(w, purchases, err)
			return
		case path == "reviews" && r.Method == "POST" && cfg.CampusReview:
			var input procurement.Input
			if !decode(w, r, &input) {
				return
			}
			report, err := procurement.Audit(input, false)
			respond(w, report, err)
			return
		}
		parts := strings.Split(path, "/")
		if len(parts) >= 2 && parts[0] == "purchases" {
			if len(parts) == 2 && r.Method == "GET" {
				purchase, err := service.Get(owner, parts[1])
				respond(w, purchase, err)
				return
			}
			if len(parts) == 3 {
				switch {
				case parts[2] == "agentpay-handoff" && r.Method == "POST":
					result, err := service.AgentPayHandoff(r.Context(), owner, parts[1])
					respond(w, result, err)
					return
				case parts[2] == "agentpay-claim" && r.Method == "POST":
					var body struct {
						Stage         string `json:"stage"`
						LinkDigest    string `json:"link_digest"`
						RecoverSignal bool   `json:"recover_signal"`
					}
					if !decode(w, r, &body) {
						return
					}
					result, err := service.ClaimAgentPay(owner, parts[1], body.Stage, body.LinkDigest, body.RecoverSignal)
					respond(w, result, err)
					return
				case parts[2] == "agentpay-result" && r.Method == "POST":
					var body struct {
						AttemptID    string `json:"attempt_id"`
						Status       string `json:"status"`
						OutputDigest string `json:"output_digest"`
						LinkDigest   string `json:"link_digest"`
					}
					if !decode(w, r, &body) {
						return
					}
					result, err := service.RecordAgentPay(owner, parts[1], body.AttemptID, body.Status, body.OutputDigest, body.LinkDigest)
					respond(w, result, err)
					return
				case parts[2] == "checkout" && r.Method == "POST":
					result, err := service.Checkout(r.Context(), owner, parts[1])
					respond(w, result, err)
					return
				case (parts[2] == "reconcile" || parts[2] == "simulate") && r.Method == "POST":
					purchase, err := service.Reconcile(r.Context(), owner, parts[1], parts[2] == "simulate")
					respond(w, purchase, err)
					return
				case parts[2] == "receipt" && r.Method == "GET":
					purchase, err := service.Get(owner, parts[1])
					if err != nil {
						respond(w, nil, err)
						return
					}
					if purchase.Delivery == nil {
						replyError(w, errors.New("支付尚未确认，报告未交付"), 409)
						return
					}
					w.Header().Set("Content-Disposition", `attachment; filename="stablepay-receipt.json"`)
					reply(w, 200, purchase)
					return
				}
			}
		}
		http.NotFound(w, r)
	})
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		replyError(w, errors.New("请求格式无效或内容过长"), 400)
		return false
	}
	return true
}
func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		reply(w, 200, value)
		return
	}
	status := 400
	if errors.Is(err, ErrNotFound) {
		status = 404
	}
	if errors.Is(err, ErrConflict) {
		status = 409
	}
	if errors.Is(err, ErrCapacity) {
		status = 503
	}
	replyError(w, err, status)
}
func replyError(w http.ResponseWriter, err error, status int) {
	reply(w, status, map[string]string{"error": err.Error()})
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
