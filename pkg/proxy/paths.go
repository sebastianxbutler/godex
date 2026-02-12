package proxy

import (
	"os"
	"path/filepath"
)

func DefaultKeysPath() string {
	return filepath.Join(defaultCodexDir(), "proxy-keys.json")
}

func DefaultStatsPath() string {
	return filepath.Join(defaultCodexDir(), "proxy-usage.jsonl")
}

func DefaultStatsSummaryPath() string {
	return filepath.Join(defaultCodexDir(), "proxy-usage.json")
}

func DefaultEventsPath() string {
	return filepath.Join(defaultCodexDir(), "proxy-events.jsonl")
}

func defaultCodexDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".codex")
	}
	return "."
}
