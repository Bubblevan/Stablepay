package observability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// RuntimeVariant is the non-secret runtime configuration that qualifies live
// benchmark evidence. ConfigHash is derived only from these public labels and
// never from API keys, DSNs or private key material.
type RuntimeVariant struct {
	RuntimeVersion   string `json:"runtime_version"`
	MemoryMode       string `json:"memory_mode"`
	RecoveryProvider string `json:"recovery_provider"`
	LLMProvider      string `json:"llm_provider"`
	ModelRef         string `json:"model_ref"`
	ConfigHash       string `json:"config_hash"`
}

func (v RuntimeVariant) Normalize() RuntimeVariant {
	v.RuntimeVersion = strings.TrimSpace(v.RuntimeVersion)
	v.MemoryMode = strings.ToLower(strings.TrimSpace(v.MemoryMode))
	v.RecoveryProvider = strings.ToLower(strings.TrimSpace(v.RecoveryProvider))
	v.LLMProvider = strings.ToLower(strings.TrimSpace(v.LLMProvider))
	v.ModelRef = strings.TrimSpace(v.ModelRef)
	return v
}

func (v RuntimeVariant) WithHash() RuntimeVariant {
	v = v.Normalize()
	if v.ConfigHash != "" {
		return v
	}
	canonical, _ := json.Marshal(struct {
		RuntimeVersion   string `json:"runtime_version"`
		MemoryMode       string `json:"memory_mode"`
		RecoveryProvider string `json:"recovery_provider"`
		LLMProvider      string `json:"llm_provider"`
		ModelRef         string `json:"model_ref"`
	}{v.RuntimeVersion, v.MemoryMode, v.RecoveryProvider, v.LLMProvider, v.ModelRef})
	digest := sha256.Sum256(canonical)
	v.ConfigHash = "sha256:" + hex.EncodeToString(digest[:])
	return v
}

// Validate qualifies a live trace. It also verifies that ConfigHash is the
// digest of the non-secret runtime variant, so a scenario label cannot be
// promoted into observed configuration.
func (v RuntimeVariant) Validate() error {
	v = v.Normalize()
	if v.RuntimeVersion == "" || (v.MemoryMode != "on" && v.MemoryMode != "off") || (v.RecoveryProvider != "llm" && v.RecoveryProvider != "rule") || v.ConfigHash == "" {
		return fmt.Errorf("incomplete runtime variant")
	}
	if v.RecoveryProvider == "llm" && (v.LLMProvider == "" || v.ModelRef == "") {
		return fmt.Errorf("LLM runtime variant is missing provider/model")
	}
	withoutHash := v
	withoutHash.ConfigHash = ""
	if withoutHash.WithHash().ConfigHash != v.ConfigHash {
		return fmt.Errorf("runtime variant config hash mismatch")
	}
	return nil
}

func (v RuntimeVariant) Matches(expected RuntimeVariant) bool {
	v = v.WithHash().Normalize()
	expected = expected.Normalize()
	if expected.RuntimeVersion != "" && v.RuntimeVersion != expected.RuntimeVersion {
		return false
	}
	if expected.MemoryMode != "" && v.MemoryMode != expected.MemoryMode {
		return false
	}
	if expected.RecoveryProvider != "" && v.RecoveryProvider != expected.RecoveryProvider {
		return false
	}
	if expected.LLMProvider != "" && v.LLMProvider != expected.LLMProvider {
		return false
	}
	if expected.ModelRef != "" && v.ModelRef != expected.ModelRef {
		return false
	}
	return expected.ConfigHash == "" || v.ConfigHash == expected.ConfigHash
}
