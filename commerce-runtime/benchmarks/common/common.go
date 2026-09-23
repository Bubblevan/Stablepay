// Package common contains the evidence and statistics primitives shared by
// the B0 resume benchmark runners. It deliberately has no dependency on the
// production Runtime so the benchmark cannot accidentally change authority.
package common

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ManifestVersion = "v1"

type BenchmarkManifest struct {
	Benchmark          string            `json:"benchmark"`
	Version            string            `json:"version"`
	DatasetHash        string            `json:"dataset_hash"`
	GitSHA             string            `json:"git_sha"`
	FrozenRuntimeSHA   string            `json:"frozen_runtime_sha,omitempty"`
	RuntimeVariant     string            `json:"runtime_variant,omitempty"`
	Seed               int64             `json:"seed"`
	Environment        map[string]string `json:"environment"`
	WorkloadConfigHash string            `json:"workload_config_hash,omitempty"`
	StartedAt          time.Time         `json:"started_at"`
	FinishedAt         time.Time         `json:"finished_at,omitempty"`
	Status             string            `json:"status"`
	Notes              []string          `json:"notes,omitempty"`
}

type RateStats struct {
	Numerator    int     `json:"numerator"`
	Denominator  int     `json:"denominator"`
	Rate         float64 `json:"rate"`
	Wilson95Low  float64 `json:"wilson_95_low"`
	Wilson95High float64 `json:"wilson_95_high"`
}

type Distribution struct {
	Count  int     `json:"count"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	P50    float64 `json:"p50"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

func NewManifest(benchmark string, seed int64, datasetHash, repoRoot, variant string, started time.Time) BenchmarkManifest {
	return BenchmarkManifest{
		Benchmark: benchmark, Version: ManifestVersion, DatasetHash: datasetHash,
		GitSHA: GitSHA(repoRoot), FrozenRuntimeSHA: "ae67ae5e48a76848b5c0dfc4a68f79eef00a5705",
		RuntimeVariant: variant, Seed: seed, Environment: Environment(), StartedAt: started.UTC(), Status: "RUNNING",
	}
}

func (m *BenchmarkManifest) Finish(status string, notes ...string) {
	if m == nil {
		return
	}
	m.FinishedAt = time.Now().UTC()
	m.Status = status
	m.Notes = append([]string(nil), notes...)
}

func Environment() map[string]string {
	env := map[string]string{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"go_version": runtime.Version(),
		"cpu_count":  strconv.Itoa(runtime.NumCPU()),
		"gomaxprocs": strconv.Itoa(runtime.GOMAXPROCS(0)),
	}
	if value := commandVersion("docker", "--version"); value != "" {
		env["docker_version"] = value
	} else {
		env["docker_version"] = "UNAVAILABLE"
	}
	if value := commandVersion("git", "--version"); value != "" {
		env["git_version"] = value
	}
	if value := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"); value != "" {
		env["mysql"] = "configured"
	} else {
		env["mysql"] = "not_configured"
	}
	if value := os.Getenv("STABLEPAY_ROCKETMQ_NAME_SERVER"); value != "" {
		env["rocketmq"] = value
	} else {
		env["rocketmq"] = "not_configured"
	}
	return env
}

func commandVersion(name string, args ...string) string {
	command := exec.Command(name, args...)
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func GitSHA(repoRoot string) string {
	if strings.TrimSpace(repoRoot) == "" {
		return "UNKNOWN"
	}
	command := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		return "UNKNOWN"
	}
	return strings.TrimSpace(string(output))
}

func EnsureDir(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("output directory is required")
	}
	return os.MkdirAll(path, 0o755)
}

func WriteJSON(path string, value any) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func WriteJSONL(path string, values []any) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			return err
		}
	}
	return file.Sync()
}

func ReadJSONL(path string, decode func([]byte) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := decode([]byte(line)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func HashJSONL(values []any) (string, error) {
	hash := sha256.New()
	for _, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func Rate(numerator, denominator int) RateStats {
	result := RateStats{Numerator: numerator, Denominator: denominator}
	if denominator <= 0 {
		return result
	}
	result.Rate = float64(numerator) / float64(denominator)
	result.Wilson95Low, result.Wilson95High = Wilson95(numerator, denominator)
	return result
}

func Wilson95(successes, trials int) (float64, float64) {
	if trials <= 0 {
		return 0, 0
	}
	p := float64(successes) / float64(trials)
	z := 1.959963984540054
	denominator := 1 + z*z/float64(trials)
	center := (p + z*z/(2*float64(trials))) / denominator
	margin := z * math.Sqrt((p*(1-p)+z*z/(4*float64(trials)))/float64(trials)) / denominator
	return math.Max(0, center-margin), math.Min(1, center+margin)
}

func Summarize(values []float64) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	total := 0.0
	for _, value := range sorted {
		total += value
	}
	return Distribution{Count: len(sorted), Mean: total / float64(len(sorted)), Median: percentile(sorted, 0.5), P50: percentile(sorted, 0.5), P95: percentile(sorted, 0.95), P99: percentile(sorted, 0.99), Min: sorted[0], Max: sorted[len(sorted)-1]}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	position := p * float64(len(sorted)-1)
	low := int(math.Floor(position))
	high := int(math.Ceil(position))
	if low == high {
		return sorted[low]
	}
	weight := position - float64(low)
	return sorted[low] + (sorted[high]-sorted[low])*weight
}

func FormatRate(rate RateStats) string {
	return fmt.Sprintf("%d/%d (%.2f%%, 95%% Wilson CI %.2f%%–%.2f%%)", rate.Numerator, rate.Denominator, rate.Rate*100, rate.Wilson95Low*100, rate.Wilson95High*100)
}
