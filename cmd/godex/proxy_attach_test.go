package main

import (
	"path/filepath"
	"testing"

	"godex/pkg/config"
)

func TestDefaultAttachTracePath_Priority(t *testing.T) {
	cfg := config.ProxyConfig{TracePath: "~/custom-trace.jsonl"}
	if got := defaultAttachTracePath(cfg); got == "" || filepath.Base(got) != "custom-trace.jsonl" {
		t.Fatalf("trace from config not used, got %q", got)
	}

	cfg = config.ProxyConfig{}
	t.Setenv("GODEX_PROXY_TRACE_PATH", "~/trace-from-env.jsonl")
	if got := defaultAttachTracePath(cfg); got == "" || filepath.Base(got) != "trace-from-env.jsonl" {
		t.Fatalf("trace from env not used, got %q", got)
	}
}

func TestDefaultAttachUpstreamAuditPath_Priority(t *testing.T) {
	cfg := config.ProxyConfig{UpstreamAuditPath: "~/custom-upstream.jsonl"}
	if got := defaultAttachUpstreamAuditPath(cfg); got == "" || filepath.Base(got) != "custom-upstream.jsonl" {
		t.Fatalf("upstream audit from config not used, got %q", got)
	}

	cfg = config.ProxyConfig{}
	t.Setenv("GODEX_UPSTREAM_AUDIT_PATH", "~/upstream-from-env.jsonl")
	if got := defaultAttachUpstreamAuditPath(cfg); got == "" || filepath.Base(got) != "upstream-from-env.jsonl" {
		t.Fatalf("upstream audit from env not used, got %q", got)
	}
}

func TestDefaultAttachEventsPath(t *testing.T) {
	cfg := config.ProxyConfig{EventsPath: "~/custom-events.jsonl"}
	if got := defaultAttachEventsPath(cfg); got == "" || filepath.Base(got) != "custom-events.jsonl" {
		t.Fatalf("events from config not used, got %q", got)
	}
}
