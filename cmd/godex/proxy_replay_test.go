package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReplayRecords_TraceAndAudit(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.jsonl")
	auditPath := filepath.Join(dir, "audit.jsonl")

	traceData := `{"ts":"2026-01-01T00:00:00Z","request_id":"px1","path":"/v1/responses","phase":"openclaw_request","payload":{"model":"m1"}}
{"ts":"2026-01-01T00:00:01Z","request_id":"px2","path":"/v1/chat/completions","phase":"openclaw_request","payload":{"model":"m2"}}
{"ts":"2026-01-01T00:00:02Z","request_id":"skip","path":"/v1/responses","phase":"other","payload":{"model":"m3"}}
`
	if err := os.WriteFile(tracePath, []byte(traceData), 0o600); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	auditData := `{"ts":"2026-01-01T00:00:03Z","request_id":"au1","path":"/v1/responses","request":{"model":"m4"}}
`
	if err := os.WriteFile(auditPath, []byte(auditData), 0o600); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	traceRecords, err := loadReplayRecords(tracePath, true)
	if err != nil {
		t.Fatalf("load trace: %v", err)
	}
	if len(traceRecords) != 2 {
		t.Fatalf("trace records = %d, want 2", len(traceRecords))
	}
	if traceRecords[1].RequestID != "px2" {
		t.Fatalf("latest trace record = %q, want px2", traceRecords[1].RequestID)
	}

	auditRecords, err := loadReplayRecords(auditPath, false)
	if err != nil {
		t.Fatalf("load audit: %v", err)
	}
	if len(auditRecords) != 1 {
		t.Fatalf("audit records = %d, want 1", len(auditRecords))
	}
}

func TestSelectReplayRecord_PreferenceAndLookup(t *testing.T) {
	traceRecords := []replayRecord{
		{Source: "trace", RequestID: "px1", Path: "/v1/responses"},
		{Source: "trace", RequestID: "px2", Path: "/v1/responses"},
	}
	auditRecords := []replayRecord{
		{Source: "audit", RequestID: "au1", Path: "/v1/responses"},
	}

	latest, err := selectReplayRecord(traceRecords, auditRecords, "latest")
	if err != nil {
		t.Fatalf("select latest: %v", err)
	}
	if latest.RequestID != "px2" {
		t.Fatalf("latest = %q, want px2", latest.RequestID)
	}

	lookup, err := selectReplayRecord(traceRecords, auditRecords, "au1")
	if err != nil {
		t.Fatalf("select by id: %v", err)
	}
	if lookup.Source != "audit" {
		t.Fatalf("source = %q, want audit", lookup.Source)
	}

	if _, err := selectReplayRecord(traceRecords, auditRecords, "missing"); err == nil {
		t.Fatalf("expected error for missing request id")
	}
}
