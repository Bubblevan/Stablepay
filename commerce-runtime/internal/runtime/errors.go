package runtime

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/repository"
)

type ErrorClass string

const (
	Retryable     ErrorClass = "RETRYABLE"
	Paused        ErrorClass = "PAUSED"
	Terminal      ErrorClass = "TERMINAL"
	Configuration ErrorClass = "CONFIGURATION"
	Invariant     ErrorClass = "INVARIANT"
)

var ErrRunnerStepLimit = errors.New("episode runner step limit reached")
var ErrEntitlementPending = errors.New("purchase entitlement is not yet visible")

var sensitiveErrorPattern = regexp.MustCompile(`(?i)(llm[_ -]?api[_ -]?key|api[_ -]?key|private[_ -]?key|payment[_ -]?signature|signed[_ -]?tx(?:base64)?|authorization)\s*[:=]\s*[^\s,;]+`)

func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ""
	}
	if errors.Is(err, episode.ErrEpisodeExpired) || errors.Is(err, payment.ErrIntentExpired) || errors.Is(err, decision.ErrPaymentIntentExpired) || errors.Is(err, episode.ErrTerminalEpisode) {
		return Terminal
	}
	if errors.Is(err, context.Canceled) {
		return Retryable
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, repository.ErrRepositoryUnavailable) || errors.Is(err, repository.ErrVersionConflict) || errors.Is(err, repository.ErrEventSequenceConflict) || errors.Is(err, payment.ErrIntentStateConflict) || errors.Is(err, invocation.ErrInvocationInFlight) || errors.Is(err, llm.ErrLLMUnavailable) || errors.Is(err, ErrRunnerStepLimit) || errors.Is(err, ErrEntitlementPending) {
		return Retryable
	}
	if errors.Is(err, adapters.ErrMerchantAdapterNotConfigured) || strings.Contains(strings.ToLower(err.Error()), "not configured") {
		return Configuration
	}
	if errors.Is(err, decision.ErrAttemptLimit) || errors.Is(err, decision.ErrActionNotAllowed) || errors.Is(err, repository.ErrFactConflict) || errors.Is(err, repository.ErrPaymentIntentConflict) {
		return Invariant
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "temporarily", "connection", "transport", "unavailable", "eof", "reset by peer", "http 5"} {
		if strings.Contains(lower, marker) {
			return Retryable
		}
	}
	return Invariant
}

func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	switch ClassifyError(err) {
	case Retryable:
		if errors.Is(err, context.DeadlineExceeded) {
			return "DEPENDENCY_TIMEOUT"
		}
		if errors.Is(err, context.Canceled) {
			return "RUNNER_CANCELED"
		}
		if errors.Is(err, llm.ErrLLMUnavailable) {
			return "LLM_UNAVAILABLE"
		}
		if errors.Is(err, repository.ErrRepositoryUnavailable) {
			return "REPOSITORY_UNAVAILABLE"
		}
		if errors.Is(err, payment.ErrIntentStateConflict) {
			return "PAYMENT_STATE_CONFLICT"
		}
		if errors.Is(err, invocation.ErrInvocationInFlight) {
			return "MERCHANT_INVOCATION_IN_FLIGHT"
		}
		if errors.Is(err, ErrRunnerStepLimit) {
			return "RUNNER_STEP_LIMIT"
		}
		if errors.Is(err, ErrEntitlementPending) {
			return "ENTITLEMENT_PENDING"
		}
		return "DEPENDENCY_UNAVAILABLE"
	case Paused:
		return "AWAITING_PARENT"
	case Terminal:
		if errors.Is(err, episode.ErrEpisodeExpired) || errors.Is(err, payment.ErrIntentExpired) || errors.Is(err, decision.ErrPaymentIntentExpired) {
			return "EPISODE_DEADLINE_EXCEEDED"
		}
		return "EPISODE_TERMINAL"
	case Configuration:
		return "CONFIGURATION_MISSING"
	default:
		return "RUNTIME_INVARIANT"
	}
}

func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	message = sensitiveErrorPattern.ReplaceAllString(message, "$1=[REDACTED]")
	message = regexp.MustCompile(`(?i)bearer\s+[^\s,;]+`).ReplaceAllString(message, "Bearer [REDACTED]")
	lower := strings.ToLower(message)
	if strings.Contains(lower, "private key") || strings.Contains(lower, "signed tx") || strings.Contains(lower, "payment signature") || strings.Contains(lower, "api key") {
		message = "dependency error details redacted"
	}
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}
