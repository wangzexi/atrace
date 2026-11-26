package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// runs.log is the one history of what the collectors did: a line when a collector is
// loaded, changed, ignored or removed, and a line for every run. Newest at the end;
// past runLogLimit bytes it moves to runs.log.1 (the older one is dropped), so a few
// months of one-line runs fit. The full output of a collector's latest run stays in
// its own <name>.log.
const (
	runLogName  = "runs.log"
	runLogLimit = 1 << 20
)

// logEvent appends one line: time, kind (load or run), the collector, the text.
// It is best effort: a log that cannot be written never stops a collector.
func logEvent(cfg settings, kind, name, text string) {
	if cfg.file == "" {
		return
	}
	path := filepath.Join(filepath.Dir(cfg.file), "logs", runLogName)
	if info, err := os.Stat(path); err == nil && info.Size() >= runLogLimit {
		_ = os.Rename(path, path+".1")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	text = strings.Join(strings.Fields(strings.ReplaceAll(text, "\x00", " ")), " ")
	if runes := []rune(text); len(runes) > 400 {
		text = string(runes[:400])
	}
	_, _ = file.WriteString(time.Now().Format(time.RFC3339) + " " + kind + " " + name + " " + text + "\n")
}

// runSummary is what a run leaves in the history: its last JSON line when it
// worked (the collector prints its counts there), else the last lines of its output.
func runSummary(output string, ok bool) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if ok {
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.HasPrefix(lines[i], "{") {
				return lines[i]
			}
		}
	}
	return strings.Join(lines[max(0, len(lines)-3):], " | ")
}
