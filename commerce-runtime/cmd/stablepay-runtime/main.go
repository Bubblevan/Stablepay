// Command stablepay-runtime is the external S10 CLI. It speaks only HTTP and
// intentionally knows nothing about payment, memory, merchant or LLM internals.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "acquire":
		err = acquire(os.Args[2:])
	case "status":
		err = status(os.Args[2:])
	case "approve":
		err = approve(os.Args[2:])
	default:
		usage()
		err = errors.New("unknown command")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "stablepay-runtime: %v\n", err)
		os.Exit(1)
	}
}

func acquire(args []string) error {
	set := flag.NewFlagSet("acquire", flag.ContinueOnError)
	server, token, file, idempotency := commonFlags(set)
	set.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: stablepay-runtime acquire --file request.json [--idempotency-key key]")
	}
	if err := set.Parse(args); err != nil {
		return err
	}
	body, err := readPayload(*file)
	if err != nil {
		return err
	}
	var request struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return err
	}
	key := strings.TrimSpace(*idempotency)
	if key == "" {
		key = strings.TrimSpace(request.RequestID)
	}
	if key == "" {
		return errors.New("request_id or --idempotency-key is required")
	}
	return call(*server, *token, http.MethodPost, "/v1/episodes", key, body)
}

func status(args []string) error {
	set := flag.NewFlagSet("status", flag.ContinueOnError)
	server, token, _, _ := commonFlags(set)
	episodeID := set.String("episode-id", "", "episode id")
	events := set.Bool("events", false, "return event log")
	if err := set.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*episodeID) == "" {
		return errors.New("--episode-id is required")
	}
	path := "/v1/episodes/" + *episodeID
	if *events {
		path += "/events"
	}
	return call(*server, *token, http.MethodGet, path, "", nil)
}

func approve(args []string) error {
	set := flag.NewFlagSet("approve", flag.ContinueOnError)
	server, token, _, idempotency := commonFlags(set)
	episodeID := set.String("episode-id", "", "episode id")
	approvalID := set.String("approval-id", "", "parent approval id")
	decision := set.String("decision", "APPROVE", "APPROVE or DENY")
	actor := set.String("actor-ref", "cli", "parent actor reference")
	traceID := set.String("trace-id", "", "trace id")
	if err := set.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*episodeID) == "" || strings.TrimSpace(*approvalID) == "" {
		return errors.New("--episode-id and --approval-id are required")
	}
	body, _ := json.Marshal(map[string]string{"approval_id": *approvalID, "decision": strings.ToUpper(*decision), "actor_ref": *actor, "trace_id": *traceID})
	key := strings.TrimSpace(*idempotency)
	if key == "" {
		key = *approvalID
	}
	return call(*server, *token, http.MethodPost, "/v1/episodes/"+*episodeID+"/parent-decisions", key, body)
}

func commonFlags(set *flag.FlagSet) (*string, *string, *string, *string) {
	server := set.String("server", envOr("COMMERCE_RUNTIME_URL", "http://127.0.0.1:8090"), "runtime server URL")
	token := set.String("token", os.Getenv("COMMERCE_RUNTIME_API_TOKEN"), "runtime API token")
	file := set.String("file", "", "JSON request file, or - for stdin")
	idempotency := set.String("idempotency-key", "", "idempotency key")
	return server, token, file, idempotency
}

func call(base, token, method, path, idempotency string, body []byte) error {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	request, err := http.NewRequest(method, base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	if strings.TrimSpace(idempotency) != "" {
		request.Header.Set("Idempotency-Key", idempotency)
	}
	client := &http.Client{Timeout: 95 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if len(result) > 0 {
		var pretty bytes.Buffer
		if json.Indent(&pretty, result, "", "  ") == nil {
			result = pretty.Bytes()
		}
	}
	fmt.Println(string(result))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("runtime returned HTTP %d", response.StatusCode)
	}
	return nil
}

func readPayload(file string) ([]byte, error) {
	if strings.TrimSpace(file) == "" || file == "-" {
		return io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	}
	return os.ReadFile(file)
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
func usage() { fmt.Fprintln(os.Stderr, "usage: stablepay-runtime {acquire|status|approve} [flags]") }
