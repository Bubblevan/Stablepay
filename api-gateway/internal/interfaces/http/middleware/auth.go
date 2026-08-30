package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"stablepay/api-gateway/internal/application"
	"stablepay/api-gateway/internal/domain"
	"stablepay/api-gateway/internal/infrastructure/auth"
	"stablepay/api-gateway/internal/infrastructure/config"
	"stablepay/api-gateway/internal/interfaces/httpresp"
)

func AuthExtract() app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		did := string(ctx.GetHeader("X-StablePay-DID"))
		signature := string(ctx.GetHeader("X-StablePay-Signature"))
		timestamp := string(ctx.GetHeader("X-StablePay-Timestamp"))
		nonce := string(ctx.GetHeader("X-StablePay-Nonce"))

		if len(ctx.Request.Body()) > 0 {
			var body map[string]interface{}
			_ = json.Unmarshal(ctx.Request.Body(), &body)
			if did == "" {
				did = asString(body["did"])
			}
			if signature == "" {
				signature = asString(body["signature"])
			}
			if timestamp == "" {
				timestamp = asString(body["timestamp"])
			}
			if nonce == "" {
				nonce = asString(body["nonce"])
			}
		}

		ctx.Set(domain.CtxDID, did)
		ctx.Set(domain.CtxSignature, signature)
		ctx.Set(domain.CtxTimestamp, timestamp)
		ctx.Set(domain.CtxNonce, nonce)
		ctx.Next(c)
	}
}

func AuthVerify(cfg config.Security, didClient application.DIDServiceClient) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		policy, ok := CurrentPolicy(ctx)
		if !ok {
			ctx.Next(c)
			return
		}

		switch policy.Auth {
		case domain.AuthNone:
			ctx.Next(c)
			return
		case domain.AuthAPI:
			apiKey := string(ctx.GetHeader("X-API-Key"))
			if !contains(cfg.AllowedAPIKeys, apiKey) {
				httpresp.Fail(ctx, domain.ErrPermissionDenied, map[string]interface{}{"detail": "invalid api key"})
				ctx.Abort()
				return
			}
		case domain.AuthDID:
			did := getCtxString(ctx, domain.CtxDID)
			signature := getCtxString(ctx, domain.CtxSignature)
			timestamp := getCtxString(ctx, domain.CtxTimestamp)
			nonce := getCtxString(ctx, domain.CtxNonce)
			if did == "" || signature == "" || timestamp == "" || nonce == "" {
				httpresp.Fail(ctx, domain.ErrSignatureVerification, map[string]interface{}{"detail": "missing did signature fields"})
				ctx.Abort()
				return
			}

			ts, err := auth.ParseTimestampFlexible(timestamp)
			if err != nil {
				httpresp.Fail(ctx, domain.ErrSignatureVerification, map[string]interface{}{"detail": err.Error()})
				ctx.Abort()
				return
			}
			if skew := time.Since(ts); skew > time.Duration(cfg.AllowedTimestampSkewS)*time.Second || skew < -time.Duration(cfg.AllowedTimestampSkewS)*time.Second {
				httpresp.Fail(ctx, domain.ErrSignatureVerification, map[string]interface{}{"detail": "timestamp out of range"})
				ctx.Abort()
				return
			}

			rawBody := ctx.Request.Body()
			bodyHash := auth.BodySHA256(rawBody)
			canonical := auth.BuildCanonicalV01(
				string(ctx.Request.Method()),
				string(ctx.Path()),
				string(ctx.URI().QueryString()),
				bodyHash,
			)
			canonicalReq := map[string]interface{}{
				"did":       did,
				"signature": signature,
				"timestamp": timestamp,
				"nonce":     nonce,
				"message":   canonical,
			}
			valid, err := didClient.VerifySignature(c, canonicalReq)
			if err != nil || !valid {
				httpresp.Fail(ctx, domain.ErrSignatureVerification, map[string]interface{}{"detail": "signature invalid"})
				ctx.Abort()
				return
			}
		}
		ctx.Next(c)
	}
}

func ReplayProtection(cfg config.Security, store auth.NonceStore) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		policy, ok := CurrentPolicy(ctx)
		if !ok || policy.Auth != domain.AuthDID {
			ctx.Next(c)
			return
		}
		nonce := getCtxString(ctx, domain.CtxNonce)
		did := getCtxString(ctx, domain.CtxDID)
		timestamp := getCtxString(ctx, domain.CtxTimestamp)
		if cfg.RequireNonce && nonce == "" {
			httpresp.Fail(ctx, domain.ErrSignatureVerification, map[string]interface{}{"detail": "nonce required"})
			ctx.Abort()
			return
		}
		if nonce == "" {
			ctx.Next(c)
			return
		}
		fingerprint := fmt.Sprintf("%s|%s|%s|%s|%s", did, nonce, timestamp, string(ctx.Path()), string(ctx.Request.Method()))
		sum := sha256.Sum256([]byte(fingerprint))
		key := hex.EncodeToString(sum[:])
		okMark, err := store.MarkIfNotExists(c, key, time.Duration(cfg.AllowedTimestampSkewS)*time.Second)
		if err != nil {
			httpresp.Fail(ctx, domain.ErrServiceUnavailable, map[string]interface{}{"detail": "nonce store unavailable"})
			ctx.Abort()
			return
		}
		if !okMark {
			httpresp.Fail(ctx, domain.ErrSignatureVerification, map[string]interface{}{"detail": "replayed request"})
			ctx.Abort()
			return
		}
		ctx.Next(c)
	}
}

func getCtxString(ctx *app.RequestContext, key string) string {
	value, exists := ctx.Get(key)
	if !exists || value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", value)
}

func asString(value interface{}) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", value)
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(target) && item != "" {
			return true
		}
	}
	return false
}
