package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"godex/pkg/config"
)

type replayRecord struct {
	Source    string
	RequestID string
	Path      string
	Timestamp string
	Payload   json.RawMessage
}

func runProxyReplay(args []string) error {
	fs := flag.NewFlagSet("proxy replay", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	cfg := config.LoadFrom(configPathFromArgs(args))
	configPath := fs.String("config", config.DefaultPath(), "Config file path")
	requestID := fs.String("request-id", "latest", "Request ID to replay (or latest)")
	listCount := fs.Int("list", 0, "List recent replayable request IDs instead of replaying")
	tracePath := fs.String("trace-path", defaultReplayTracePath(cfg.Proxy.TracePath), "Trace JSONL path")
	auditPath := fs.String("audit-path", defaultReplayAuditPath(cfg.Proxy.AuditPath), "Audit JSONL path")
	url := fs.String("url", "http://127.0.0.1:39001", "Proxy base URL")
	apiKey := fs.String("api-key", "", "Bearer API key (defaults to config/env/openclaw config)")
	savePayload := fs.String("save-payload", "", "Write replay payload JSON to file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = configPath

	if fs.NArg() > 0 {
		*requestID = fs.Arg(0)
	}

	traceRecords, traceErr := loadReplayRecords(*tracePath, true)
	auditRecords, auditErr := loadReplayRecords(*auditPath, false)
	if len(traceRecords) == 0 && len(auditRecords) == 0 {
		if traceErr != nil {
			return traceErr
		}
		if auditErr != nil {
			return auditErr
		}
		return fmt.Errorf("no replayable requests found in %s or %s", *tracePath, *auditPath)
	}

	if *listCount > 0 {
		if err := printReplayList(traceRecords, auditRecords, *listCount); err != nil {
			return err
		}
		return nil
	}

	rec, err := selectReplayRecord(traceRecords, auditRecords, strings.TrimSpace(*requestID))
	if err != nil {
		return err
	}

	if strings.TrimSpace(*savePayload) != "" {
		if err := os.WriteFile(*savePayload, rec.Payload, 0o600); err != nil {
			return fmt.Errorf("write payload: %w", err)
		}
		fmt.Fprintf(os.Stderr, "saved payload to: %s\n", *savePayload)
	}

	key := strings.TrimSpace(*apiKey)
	if key == "" {
		key = strings.TrimSpace(cfg.Proxy.APIKey)
	}
	if key == "" {
		key = strings.TrimSpace(os.Getenv("OPENCLAW_BEARER_TOKEN"))
	}
	if key == "" {
		key = readOpenClawProviderKey()
	}

	endpoint := strings.TrimRight(*url, "/") + rec.Path
	fmt.Fprintf(os.Stderr, "replaying source=%s request_id=%s path=%s\n", rec.Source, rec.RequestID, rec.Path)

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(rec.Payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("replay request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		return fmt.Errorf("replay failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		return fmt.Errorf("stream replay response: %w", err)
	}
	fmt.Println()
	return nil
}

func defaultReplayTracePath(path string) string {
	if strings.TrimSpace(path) != "" {
		return expandHome(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, ".godex", "proxy-trace.jsonl")
}

func defaultReplayAuditPath(path string) string {
	if strings.TrimSpace(path) != "" {
		return expandHome(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, ".godex", "audit.jsonl")
}

func loadReplayRecords(path string, fromTrace bool) ([]replayRecord, error) {
	path = expandHome(strings.TrimSpace(path))
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var out []replayRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 20*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if fromTrace {
			var rec struct {
				Timestamp string          `json:"ts"`
				RequestID string          `json:"request_id"`
				Path      string          `json:"path"`
				Phase     string          `json:"phase"`
				Payload   json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(line, &rec); err != nil {
				continue
			}
			if rec.Phase != "openclaw_request" || !isReplayPath(rec.Path) || len(rec.Payload) == 0 {
				continue
			}
			out = append(out, replayRecord{
				Source:    "trace",
				RequestID: rec.RequestID,
				Path:      rec.Path,
				Timestamp: rec.Timestamp,
				Payload:   rec.Payload,
			})
			continue
		}

		var rec struct {
			Timestamp string          `json:"ts"`
			RequestID string          `json:"request_id"`
			Path      string          `json:"path"`
			Request   json.RawMessage `json:"request"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if !isReplayPath(rec.Path) || len(rec.Request) == 0 {
			continue
		}
		out = append(out, replayRecord{
			Source:    "audit",
			RequestID: rec.RequestID,
			Path:      rec.Path,
			Timestamp: rec.Timestamp,
			Payload:   rec.Request,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

func isReplayPath(path string) bool {
	return path == "/v1/responses" || path == "/v1/chat/completions"
}

func selectReplayRecord(traceRecords, auditRecords []replayRecord, requestID string) (replayRecord, error) {
	if requestID == "" || requestID == "latest" {
		if len(traceRecords) > 0 {
			return traceRecords[len(traceRecords)-1], nil
		}
		if len(auditRecords) > 0 {
			return auditRecords[len(auditRecords)-1], nil
		}
		return replayRecord{}, fmt.Errorf("no replay records found")
	}
	for i := len(traceRecords) - 1; i >= 0; i-- {
		if traceRecords[i].RequestID == requestID {
			return traceRecords[i], nil
		}
	}
	for i := len(auditRecords) - 1; i >= 0; i-- {
		if auditRecords[i].RequestID == requestID {
			return auditRecords[i], nil
		}
	}
	return replayRecord{}, fmt.Errorf("request_id not found: %s", requestID)
}

func printReplayList(traceRecords, auditRecords []replayRecord, n int) error {
	if n <= 0 {
		n = 20
	}
	var src []replayRecord
	if len(traceRecords) > 0 {
		src = traceRecords
	} else {
		src = auditRecords
	}
	if len(src) == 0 {
		return fmt.Errorf("no replayable requests found")
	}
	start := 0
	if len(src) > n {
		start = len(src) - n
	}
	for _, rec := range src[start:] {
		fmt.Printf("%s\t%s\t%s\t%s\n", rec.RequestID, rec.Path, rec.Timestamp, rec.Source)
	}
	return nil
}

func readOpenClawProviderKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".openclaw", "openclaw.json")
	buf, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		Models struct {
			Providers struct {
				Godex struct {
					APIKey string `json:"apiKey"`
				} `json:"godex"`
			} `json:"providers"`
		} `json:"models"`
	}
	if err := json.Unmarshal(buf, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Models.Providers.Godex.APIKey)
}
