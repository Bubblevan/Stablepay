package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/stablepay/commerce-runtime/internal/llm"
)

func TestErrorClassificationAndSanitization(t *testing.T) {
	if got := ClassifyError(llm.ErrLLMUnavailable); got != Retryable {
		t.Fatalf("LLM outage class=%s", got)
	}
	if got := ErrorCode(llm.ErrLLMUnavailable); got != "LLM_UNAVAILABLE" {
		t.Fatalf("LLM outage code=%s", got)
	}
	message := SanitizeError(errors.New("LLM_API_KEY=secret private key=hidden payment-signature=opaque " + strings.Repeat("x", 600)))
	if len(message) > 512 || strings.Contains(message, "secret") || strings.Contains(message, "hidden") || strings.Contains(message, "opaque") {
		t.Fatalf("sensitive error was not sanitized: %q", message)
	}
}
