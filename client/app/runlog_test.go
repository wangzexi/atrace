package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readRunLog(t *testing.T, cfg settings) string {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(filepath.Dir(cfg.file), "logs", runLogName))
	return string(raw)
}

func TestRunLogRecordsLoadingAndRuns(t *testing.T) {
	cfg := testSettings(t)
	if err := os.MkdirAll(filepath.Join(filepath.Dir(cfg.file), "logs"), 0700); err != nil {
		t.Fatal(err)
	}
	putPlugin(t, cfg, fixturePlugin(), "console.log('{\"saved\":2}')\n")
	putPlugin(t, cfg, plugin{ID: "badcron", Cron: "not a cron"}, "")
	putPlugin(t, cfg, plugin{ID: "noentry", Cron: "0 * * * *"}, "")
	if err := os.Remove(filepath.Join(cfg.directory, "noentry", "index.mjs")); err != nil {
		t.Fatal(err)
	}
	c := newCollectors(cfg, func(string) {})
	// the scheduler reloads every few seconds: the same problem is written once
	c.reload(cfg)
	c.reload(cfg)
	c.reload(cfg)
	log := readRunLog(t, cfg)
	for _, want := range []string{" load fixture loaded: cron", " load badcron ignored: cron", " load noentry ignored: no index.mjs"} {
		if strings.Count(log, want) != 1 {
			t.Fatalf("want %q once in:\n%s", want, log)
		}
	}
	c.run(context.Background(), cfg, *c.states["fixture"], "")
	c.run(context.Background(), cfg, *c.states["fixture"], "fixture:post:1")
	log = readRunLog(t, cfg)
	if !strings.Contains(log, ` run fixture schedule ok `) || !strings.Contains(log, `{"saved":2}`) {
		t.Fatalf("scheduled run not recorded:\n%s", log)
	}
	if !strings.Contains(log, ` run fixture open fixture:post:1 ok `) {
		t.Fatalf("run for an opened post not recorded:\n%s", log)
	}
	// fixing the problem is a line too, and so is removing a collector
	putPlugin(t, cfg, plugin{ID: "badcron", Cron: "0 5 * * *"}, "")
	if err := os.RemoveAll(filepath.Join(cfg.directory, "fixture")); err != nil {
		t.Fatal(err)
	}
	c.reload(cfg)
	log = readRunLog(t, cfg)
	if !strings.Contains(log, " load badcron loaded: cron") || !strings.Contains(log, " load fixture removed") {
		t.Fatalf("recovery or removal not recorded:\n%s", log)
	}
}

func TestRunLogRotatesAtTheLimit(t *testing.T) {
	cfg := testSettings(t)
	dir := filepath.Join(filepath.Dir(cfg.file), "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runLogName), make([]byte, runLogLimit), 0600); err != nil {
		t.Fatal(err)
	}
	logEvent(cfg, "run", "fixture", "schedule ok")
	if info, err := os.Stat(filepath.Join(dir, runLogName+".1")); err != nil || info.Size() != runLogLimit {
		t.Fatalf("full log not moved aside: %v", err)
	}
	if !strings.Contains(readRunLog(t, cfg), "schedule ok") || len(readRunLog(t, cfg)) > 200 {
		t.Fatal("new log did not start fresh")
	}
}
