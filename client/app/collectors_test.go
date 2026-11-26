package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func testSettings(t *testing.T) settings {
	t.Helper()
	home := t.TempDir()
	return settings{home: home, directory: filepath.Join(home, "collectors"), file: filepath.Join(home, "collector-server"), server: "http://127.0.0.1:8183", env: map[string]string{"PATH": os.Getenv("PATH"), "ATRACE_COLLECT_TOKEN": "fixture-token"}}
}
func putPlugin(t *testing.T, cfg settings, p plugin, script string) {
	t.Helper()
	folder := filepath.Join(cfg.directory, p.ID)
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	if err := os.WriteFile(filepath.Join(folder, "config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "index.mjs"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
}
func fixturePlugin() plugin {
	return plugin{ID: "fixture", Cron: "0 * * * *"}
}
func TestSettingsAddressAndTokens(t *testing.T) {
	for _, input := range []string{"http://evil.example", "https://user:pass@example.com", "https://example.com/path", "https://example.com/?token=x", "https://example.com/#x", ""} {
		if _, err := serverAddress(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	for _, input := range []string{"https://example.com/", "http://localhost:8183", "http://[::1]:8183"} {
		if _, err := serverAddress(input); err != nil {
			t.Errorf("rejected %q", input)
		}
	}
	cfg := testSettings(t)
	cfg.env["ATRACE_REMOTE_COLLECT_TOKEN"] = "remote-token"
	if cfg.token() != "fixture-token" {
		t.Fatal("wrong local token")
	}
	cfg.server = "https://example.com"
	if cfg.token() != "remote-token" {
		t.Fatal("wrong remote token")
	}
	if err := cfg.saveServer(cfg.server); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg.file)
	var saved appConfig
	_ = json.Unmarshal(b, &saved)
	if saved.Server != cfg.server {
		t.Fatal("server not saved")
	}
}
func TestCollectorHotReloadAndRefreshDedup(t *testing.T) {
	cfg := testSettings(t)
	p := fixturePlugin()
	putPlugin(t, cfg, p, "")
	c := newCollectors(cfg, func(string) {})
	c.reload(cfg)
	if len(c.states) != 1 {
		t.Fatal("plugin not discovered")
	}
	c.states[p.ID].account = "fixture:user:1"
	c.reload(cfg)
	if c.states[p.ID].account != "fixture:user:1" {
		t.Fatal("unchanged plugin lost state")
	}
	w := want{p.ID, "fixture:post:1"}
	c.enqueue(w)
	c.enqueue(w)
	if len(c.queue) != 1 {
		t.Fatal("duplicate refresh")
	}
	s, target := c.next(time.Now())
	if s == nil || target != w.Post {
		t.Fatal("refresh was not prioritized")
	}
	c.enqueue(w)
	if len(c.queue) != 0 {
		t.Fatal("running refresh duplicated")
	}
	p.Paused = true
	putPlugin(t, cfg, p, "")
	c.reload(cfg)
	if len(c.platforms()) != 0 {
		t.Fatal("paused plugin subscribed")
	}
	if err := os.RemoveAll(filepath.Join(cfg.directory, p.ID)); err != nil {
		t.Fatal(err)
	}
	c.reload(cfg)
	if len(c.states) != 0 {
		t.Fatal("removed plugin retained")
	}
}
func TestCollectorExecutionUsesDestinationAndPreservesResult(t *testing.T) {
	cfg := testSettings(t)
	p := fixturePlugin()
	script := "if (process.env.ATRACE_SERVER !== 'http://127.0.0.1:8183' || process.env.ATRACE_COLLECT_TOKEN !== 'fixture-token' || process.env.ATRACE_REMOTE_COLLECT_TOKEN) process.exit(3)\n" +
		"console.log('ATRACE_ACCOUNT fixture:user:1')\nconsole.log('{\"saved\":1}')\n"
	putPlugin(t, cfg, p, script)
	c := newCollectors(cfg, func(string) {})
	c.reload(cfg)
	original := *c.states[p.ID]
	c.run(context.Background(), cfg, original, "")
	state := c.states[p.ID]
	if state.state != "ok" || state.account != "fixture:user:1" || state.message != `{"saved":1}` || state.next.Before(time.Now()) {
		t.Fatalf("unexpected result: %+v", state)
	}
	putPlugin(t, cfg, p, "console.log('captcha'); process.exit(1)\n")
	c.reload(cfg)
	c.run(context.Background(), cfg, *c.states[p.ID], "")
	if c.states[p.ID].state != "blocked" {
		t.Fatal("captcha not blocked")
	}
}
func TestCollectorCancellationReapsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `sleep 120 & echo $! > "$1"; wait`, "fixture", pidFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = time.Second
	_, ok := executeCollector(ctx, cmd)
	if ok {
		t.Fatal("cancelled collector succeeded")
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	fmt.Sscan(string(raw), &pid)
	// A reaped parent may leave a short-lived zombie for init; it cannot keep working.
	if err := syscall.Kill(pid, 0); err == nil {
		out, _ := exec.Command("ps", "-p", fmt.Sprint(pid), "-o", "stat=").Output()
		if !strings.Contains(string(out), "Z") && strings.TrimSpace(string(out)) != "" {
			t.Fatalf("collector child still alive: %d %s", pid, out)
		}
	}
}
func TestServerSessionReportsSubscribesAndSwitches(t *testing.T) {
	cfg := testSettings(t)
	p := fixturePlugin()
	putPlugin(t, cfg, p, "")
	reported := make(chan string, 8)
	subscribed := make(chan struct{}, 8)
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			http.Error(w, "token", 401)
			return
		}
		switch r.URL.Path {
		case "/collect/status":
			var row struct {
				Device     string              `json:"device"`
				Collectors []map[string]string `json:"collectors"`
			}
			if json.NewDecoder(r.Body).Decode(&row) != nil || row.Device == "" {
				http.Error(w, "body", 400)
				return
			}
			reported <- r.Host
			w.WriteHeader(204)
		case "/collect/wants":
			if r.URL.Query().Get("platforms") != "fixture" {
				http.Error(w, "platforms", 400)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"platform\":\"fixture\",\"post\":\"fixture:post:2\"}\n\n")
			w.(http.Flusher).Flush()
			subscribed <- struct{}{}
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}
	first := httptest.NewServer(http.HandlerFunc(handler))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(handler))
	defer second.Close()
	cfg.server = first.URL
	c := newCollectors(cfg, func(string) {})
	c.reload(cfg)
	c.states[p.ID].next = time.Now().Add(time.Hour)
	c.start()
	defer c.stop()
	select {
	case <-reported:
	case <-time.After(2 * time.Second):
		t.Fatal("no heartbeat")
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no refresh subscription")
	}
	if err := c.setServer(second.URL); err != nil {
		t.Fatal(err)
	}
	select {
	case host := <-reported:
		if !strings.Contains(second.URL, host) {
			t.Fatal("wrong destination")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no new heartbeat")
	}
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("no new subscription")
	}
	c.mu.Lock()
	for _, w := range c.queue {
		if w.Post != "fixture:post:2" {
			t.Fatal("unexpected queued post")
		}
	}
	c.mu.Unlock()
}

func TestBundledBunWorksWithoutUserShellPath(t *testing.T) {
	cfg := testSettings(t)
	cfg.bin = filepath.Join(cfg.home, "Atrace.app", "Contents", "MacOS")
	bin := filepath.Join(cfg.home, "Atrace.app", "Contents", "Resources", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "bun"), []byte("#!/bin/sh\nprintf '{\"bundled\":true}\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	cfg.env["PATH"] = "/usr/bin:/bin"
	p := fixturePlugin()
	putPlugin(t, cfg, p, "")
	c := newCollectors(cfg, func(string) {})
	c.reload(cfg)
	c.run(context.Background(), cfg, *c.states[p.ID], "")
	if c.states[p.ID].state != "ok" || c.states[p.ID].message != `{"bundled":true}` {
		t.Fatalf("bundled runtime not used: %+v", c.states[p.ID])
	}
}

func TestSettingsIsolationAndPersistence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATRACE_HOME", "")
	t.Setenv("ATRACE_URL", "")
	t.Setenv("ATRACE_COLLECTORS", "")
	root := filepath.Join(home, ".atrace")
	if err := writeConfig(filepath.Join(root, "config.json"), appConfig{Server: "https://example.com", RemoteCollectToken: "fixture-secret", DevHost: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.server != "https://example.com" || cfg.directory != filepath.Join(root, "collectors") {
		t.Fatal("wrong settings")
	}
	if err := cfg.saveServer("https://other.example"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfg.file)
	var saved appConfig
	json.Unmarshal(raw, &saved)
	if saved.RemoteCollectToken != "fixture-secret" || saved.DevHost != "127.0.0.1" {
		t.Fatal("save lost settings")
	}
	t.Setenv("ATRACE_HOME", filepath.Join(home, "isolated"))
	isolated, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if isolated.server != "http://127.0.0.1:8183" {
		t.Fatal("isolation imported production settings")
	}
	raw, _ = os.ReadFile(isolated.file)
	var blank appConfig
	json.Unmarshal(raw, &blank)
	if blank.RemoteCollectToken != "" {
		t.Fatal("isolation imported credentials")
	}
}

func TestCronUsesWallClockBoundaries(t *testing.T) {
	cfg := testSettings(t)
	p := plugin{ID: "fixture", Cron: "0 */6 * * *"}
	putPlugin(t, cfg, p, "")
	c := newCollectors(cfg, func(string) {})
	c.reload(cfg)
	state := c.states[p.ID]
	if state == nil {
		t.Fatal("cron collector was not discovered")
	}
	due := state.next
	if due.Hour()%6 != 0 || due.Minute() != 0 || due.Second() != 0 {
		t.Fatalf("expected a six-hour wall-clock boundary, got %s", due)
	}
}

func TestCollectorNeedsConfigCronAndEntry(t *testing.T) {
	cfg := testSettings(t)
	// a package.json alone, the old layout, is not a collector
	dir := filepath.Join(cfg.directory, "old")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"old","scripts":{"start":"bun index.mjs"},"atrace":{"cron":"0 */6 * * *"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	// config without index.mjs is not a collector either
	putPlugin(t, cfg, plugin{ID: "noentry", Cron: "0 */6 * * *"}, "")
	if err := os.Remove(filepath.Join(cfg.directory, "noentry", "index.mjs")); err != nil {
		t.Fatal(err)
	}
	// a bad cron is skipped
	putPlugin(t, cfg, plugin{ID: "badcron", Cron: "not a cron"}, "")
	c := newCollectors(cfg, func(string) {})
	c.reload(cfg)
	if len(c.states) != 0 {
		t.Fatalf("expected no collectors, got %v", c.states)
	}
}

func TestCollectorReceivesTargetThroughEnvironment(t *testing.T) {
	cfg := testSettings(t)
	p := fixturePlugin()
	putPlugin(t, cfg, p, "")
	c := newCollectors(cfg, func(string) {})
	c.reload(cfg)
	const post = "fixture:post:123"
	c.enqueue(want{Platform: "fixture", Post: post})
	state, target := c.next(time.Now())
	if state == nil || target != post || state.plugin.ID != "fixture" {
		t.Fatalf("expected fixture with target %s, got %v / %s", post, state, target)
	}
}
