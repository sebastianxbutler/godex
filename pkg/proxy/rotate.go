package proxy

import (
	"fmt"
	"os"
	"path/filepath"
)

func rotateFile(path string, maxBackups int) error {
	if maxBackups <= 0 {
		return nil
	}
	// shift: .(maxBackups-1) -> .maxBackups
	for i := maxBackups - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", path, i)
		to := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	// rotate current
	if _, err := os.Stat(path); err == nil {
		_ = os.Rename(path, fmt.Sprintf("%s.1", path))
	}
	// ensure parent dir exists
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0o700)
	return nil
}
