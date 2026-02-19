package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"godex/pkg/config"
	"godex/pkg/proxy"
)

func runProxyAttach(args []string) error {
	fs := flag.NewFlagSet("proxy attach", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	cfg := config.LoadFrom(configPathFromArgs(args))

	configPath := fs.String("config", config.DefaultPath(), "Config file path")
	service := fs.String("service", "godex-proxy.service", "systemd user service name")
	journal := fs.Bool("journal", true, "Attach to systemd journal stream")
	trace := fs.Bool("trace", true, "Attach to proxy trace JSONL stream")
	upstreamAudit := fs.Bool("upstream-audit", true, "Attach to upstream SSE audit JSONL stream")
	events := fs.Bool("events", false, "Attach to proxy events JSONL stream")
	noJournal := fs.Bool("no-journal", false, "Disable journal stream")
	noTrace := fs.Bool("no-trace", false, "Disable trace stream")
	noUpstreamAudit := fs.Bool("no-upstream-audit", false, "Disable upstream audit stream")
	noEvents := fs.Bool("no-events", false, "Disable events stream")
	tracePath := fs.String("trace-path", defaultAttachTracePath(cfg.Proxy), "Proxy trace JSONL path")
	upstreamAuditPath := fs.String("upstream-audit-path", defaultAttachUpstreamAuditPath(cfg.Proxy), "Upstream SSE audit JSONL path")
	eventsPath := fs.String("events-path", defaultAttachEventsPath(cfg.Proxy), "Proxy events JSONL path")
	since := fs.String("since", "", "Journal lookback (example: '10 min ago' or '2026-02-18 20:00:00')")
	journalLines := fs.Int("journal-lines", 40, "Recent journal lines when --since is empty")
	grepFilter := fs.String("grep", "", "Only print lines containing this text")

	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = configPath

	if *noJournal {
		*journal = false
	}
	if *noTrace {
		*trace = false
	}
	if *noUpstreamAudit {
		*upstreamAudit = false
	}
	if *noEvents {
		*events = false
	}
	if !*journal && !*trace && !*upstreamAudit && !*events {
		return errors.New("no streams enabled; use at least one of --journal, --trace, --upstream-audit, or --events")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintln(os.Stderr, "attaching to local proxy streams (Ctrl-C to detach)")

	var mu sync.Mutex
	printLine := func(label, line string) {
		if strings.TrimSpace(*grepFilter) != "" && !strings.Contains(line, *grepFilter) {
			return
		}
		mu.Lock()
		fmt.Fprintf(os.Stdout, "[%s] %s\n", label, line)
		mu.Unlock()
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 4)

	if *journal {
		journalArgs := []string{"--user", "-fu", strings.TrimSpace(*service)}
		if strings.TrimSpace(*since) != "" {
			journalArgs = append(journalArgs, "--since", strings.TrimSpace(*since))
		} else {
			journalArgs = append(journalArgs, "-n", strconv.Itoa(*journalLines))
		}
		startAttachCommand(ctx, &wg, errCh, "journal", "journalctl", journalArgs, printLine)
	}
	if *trace {
		startAttachCommand(ctx, &wg, errCh, "trace", "tail", []string{"-n", "0", "-F", strings.TrimSpace(*tracePath)}, printLine)
	}
	if *upstreamAudit {
		startAttachCommand(ctx, &wg, errCh, "upstream-audit", "tail", []string{"-n", "0", "-F", strings.TrimSpace(*upstreamAuditPath)}, printLine)
	}
	if *events {
		startAttachCommand(ctx, &wg, errCh, "events", "tail", []string{"-n", "0", "-F", strings.TrimSpace(*eventsPath)}, printLine)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case err := <-errCh:
		stop()
		<-done
		return err
	case <-ctx.Done():
		<-done
		return nil
	case <-done:
		return nil
	}
}

func defaultAttachTracePath(cfg config.ProxyConfig) string {
	if strings.TrimSpace(cfg.TracePath) != "" {
		return expandHome(cfg.TracePath)
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_PROXY_TRACE_PATH")); v != "" {
		return expandHome(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".godex", "proxy-trace.jsonl")
}

func defaultAttachUpstreamAuditPath(cfg config.ProxyConfig) string {
	if strings.TrimSpace(cfg.UpstreamAuditPath) != "" {
		return expandHome(cfg.UpstreamAuditPath)
	}
	if v := strings.TrimSpace(os.Getenv("GODEX_UPSTREAM_AUDIT_PATH")); v != "" {
		return expandHome(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".godex", "upstream-audit.jsonl")
}

func defaultAttachEventsPath(cfg config.ProxyConfig) string {
	if strings.TrimSpace(cfg.EventsPath) != "" {
		return expandHome(cfg.EventsPath)
	}
	return proxy.DefaultEventsPath()
}

func startAttachCommand(ctx context.Context, wg *sync.WaitGroup, errCh chan<- error, label, cmd string, args []string, emit func(label, line string)) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		proc := exec.CommandContext(ctx, cmd, args...)
		stdout, err := proc.StdoutPipe()
		if err != nil {
			select {
			case errCh <- fmt.Errorf("%s stdout pipe: %w", label, err):
			default:
			}
			return
		}
		stderr, err := proc.StderrPipe()
		if err != nil {
			select {
			case errCh <- fmt.Errorf("%s stderr pipe: %w", label, err):
			default:
			}
			return
		}
		if err := proc.Start(); err != nil {
			select {
			case errCh <- fmt.Errorf("%s start failed: %w", label, err):
			default:
			}
			return
		}

		var scanWG sync.WaitGroup
		scanWG.Add(2)
		go func() {
			defer scanWG.Done()
			scanAttachStream(label, stdout, emit)
		}()
		go func() {
			defer scanWG.Done()
			scanAttachStream(label, stderr, emit)
		}()

		waitErr := proc.Wait()
		scanWG.Wait()
		if ctx.Err() != nil {
			return
		}
		if waitErr != nil {
			select {
			case errCh <- fmt.Errorf("%s exited: %w", label, waitErr):
			default:
			}
		}
	}()
}

func scanAttachStream(label string, r io.Reader, emit func(label, line string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		emit(label, line)
	}
}
