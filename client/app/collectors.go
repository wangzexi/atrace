package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"
)

// A collector is a folder holding index.mjs, run as "bun index.mjs", and config.json.
// config.json holds everything the owner sets: cron (five fields, device time zone) and paused.
// The folder name is the platform ID.
type plugin struct {
	ID     string `json:"-"`
	Cron   string `json:"cron"`
	Paused bool   `json:"paused"`
}

const collectorEntry = "index.mjs"

func readCollectorConfig(path string) (plugin, error) {
	var p plugin
	raw, err := os.ReadFile(filepath.Join(path, "config.json"))
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p)
	p.ID = filepath.Base(path)
	return p, err
}
func (p plugin) eligible(state string) bool {
	return !p.Paused && state != "blocked" && state != "login"
}

type collectorState struct {
	plugin                  plugin
	schedule                cron.Schedule
	folder                  string
	modified                time.Time
	state, message, account string
	last, next              time.Time
	failures                int
}
type want struct {
	Platform string `json:"platform"`
	Post     string `json:"post"`
}

// One session owns a server connection and all its workers. Switching server cancels
// and reaps the old session before starting another, so work cannot cross destinations.
type collectors struct {
	lifecycle         sync.Mutex
	closed            bool
	mu                sync.Mutex
	cfg               settings
	cancel            context.CancelFunc
	done              chan struct{}
	states            map[string]*collectorState
	queue             []want
	problems          map[string]string
	running           want
	connectionChanged func(string)
}

func newCollectors(cfg settings, changed func(string)) *collectors {
	return &collectors{cfg: cfg, states: map[string]*collectorState{}, problems: map[string]string{}, connectionChanged: changed}
}
func (c *collectors) config() settings { c.mu.Lock(); defer c.mu.Unlock(); return c.cfg }
func (c *collectors) start()           { c.lifecycle.Lock(); defer c.lifecycle.Unlock(); c.startSession() }
func (c *collectors) startSession() {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.done = make(chan struct{})
	cfg := c.config()
	go func() {
		defer close(c.done)
		var wg sync.WaitGroup
		wg.Go(func() { c.schedule(ctx, cfg) })
		wg.Go(func() { c.report(ctx, cfg) })
		wg.Go(func() { c.subscribe(ctx, cfg) })
		wg.Wait()
	}()
}
func (c *collectors) stopSession() {
	if c.cancel != nil {
		c.cancel()
		<-c.done
		c.cancel = nil
	}
}
func (c *collectors) stop() {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.closed = true
	c.stopSession()
}
func (c *collectors) setServer(address string) error {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	if c.closed {
		return errors.New("采集器已退出")
	}
	cfg := c.config()
	if cfg.server == address {
		return nil
	}
	if err := cfg.saveServer(address); err != nil {
		return err
	}
	c.stopSession()
	c.mu.Lock()
	c.cfg.server = address
	c.queue = nil
	c.running = want{}
	c.mu.Unlock()
	c.connectionChanged("正在连接…")
	c.startSession()
	return nil
}

func (c *collectors) reload(cfg settings) {
	folders, _ := os.ReadDir(cfg.directory)
	c.mu.Lock()
	defer c.mu.Unlock()
	next := map[string]*collectorState{}
	problems := map[string]string{}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	for _, folder := range folders {
		if !folder.IsDir() || strings.HasPrefix(folder.Name(), ".") {
			continue
		}
		path := filepath.Join(cfg.directory, folder.Name())
		p, err := readCollectorConfig(path)
		if err != nil {
			problems[folder.Name()] = "ignored: config.json: " + err.Error()
			continue
		}
		schedule, err := parser.Parse(p.Cron)
		if err != nil {
			problems[folder.Name()] = "ignored: cron " + strconv.Quote(p.Cron) + ": " + err.Error()
			continue
		}
		if schedule.Next(time.Now()).IsZero() {
			problems[folder.Name()] = "ignored: cron " + strconv.Quote(p.Cron) + " never runs"
			continue
		}
		if _, err := os.Stat(filepath.Join(path, collectorEntry)); err != nil {
			problems[folder.Name()] = "ignored: no " + collectorEntry
			continue
		}
		var modified time.Time
		_ = filepath.WalkDir(path, func(name string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if !entry.IsDir() && (strings.HasSuffix(name, ".mjs") || strings.HasSuffix(name, ".js") || entry.Name() == "config.json") {
				if info, err := entry.Info(); err == nil && info.ModTime().After(modified) {
					modified = info.ModTime()
				}
			}
			return nil
		})
		if old := c.states[p.ID]; old != nil && reflect.DeepEqual(old.plugin, p) && old.modified.Equal(modified) {
			next[p.ID] = old
			continue
		}
		next[p.ID] = &collectorState{plugin: p, schedule: schedule, folder: path, modified: modified, state: "idle", next: schedule.Next(time.Now())}
		what := "loaded"
		if c.states[p.ID] != nil {
			what = "changed"
		}
		logEvent(cfg, "load", p.ID, what+": cron "+strconv.Quote(p.Cron)+", paused "+strconv.FormatBool(p.Paused))
	}
	// A problem is written once, when it appears or changes, not on every pass (a pass is every 3 seconds).
	for name, text := range problems {
		if c.problems[name] != text {
			logEvent(cfg, "load", name, text)
		}
	}
	for name := range c.states {
		if next[name] == nil && problems[name] == "" {
			logEvent(cfg, "load", name, "removed")
		}
	}
	c.problems = problems
	c.states = next
}

func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (c *collectors) schedule(ctx context.Context, cfg settings) {
	var timedEnd time.Time
	for ctx.Err() == nil {
		c.reload(cfg)
		if cfg.token() != "" {
			state, target := c.next(timedEnd)
			if state != nil {
				c.run(ctx, cfg, *state, target)
				if target == "" {
					timedEnd = time.Now()
				}
			}
		}
		if !wait(ctx, 3*time.Second) {
			return
		}
	}
}
func (c *collectors) next(timedEnd time.Time) (*collectorState, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.queue) > 0 {
		w := c.queue[0]
		c.queue = c.queue[1:]
		if s := c.states[w.Platform]; s != nil && s.plugin.eligible(s.state) {
			c.running = w
			copy := *s
			return &copy, w.Post
		}
	}
	if time.Since(timedEnd) < 2*time.Minute {
		return nil, ""
	}
	var next *collectorState
	for _, s := range c.states {
		if s.plugin.eligible(s.state) && !s.next.After(time.Now()) && (next == nil || s.next.Before(next.next)) {
			next = s
		}
	}
	if next == nil {
		return nil, ""
	}
	next.state = "running"
	copy := *next
	return &copy, ""
}

var blockedPattern = regexp.MustCompile(`(?i)risk control|captcha|验证|\b(403|429|300011)\b|风控`)
var loginPattern = regexp.MustCompile(`(?i)not logged in|sign in|登录`)

const collectorTimeout = time.Hour

func (c *collectors) run(ctx context.Context, cfg settings, original collectorState, target string) {
	childCtx, cancel := context.WithTimeout(ctx, collectorTimeout)
	defer cancel()
	cmd := exec.CommandContext(childCtx, "bun", collectorEntry)
	cmd.Dir = original.folder
	// A separate process group also owns helper processes started by the collector.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	env := map[string]string{}
	for k, v := range cfg.env {
		env[k] = v
	}
	env["PATH"] = strings.Join([]string{filepath.Clean(filepath.Join(cfg.bin, "../Resources/bin")), cfg.bin, filepath.Join(cfg.home, ".local/bin"), filepath.Join(cfg.home, ".bun/bin"), "/opt/homebrew/bin", "/usr/local/bin", env["PATH"]}, string(os.PathListSeparator))
	env["ATRACE_SERVER"] = cfg.server
	env["ATRACE_COLLECT_TOKEN"] = cfg.token()
	delete(env, "ATRACE_REMOTE_COLLECT_TOKEN")
	delete(env, "ATRACE_TARGET")
	delete(env, "ATRACE_FULL")
	if target != "" {
		env["ATRACE_TARGET"] = target
	}
	// exec.Command resolves against the parent's PATH, which Finder does not
	// populate with our bundled Bun. Resolve against the child's PATH instead.
	for _, dir := range filepath.SplitList(env["PATH"]) {
		path := filepath.Join(dir, "bun")
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			cmd.Path, cmd.Err = path, nil
			break
		}
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	trigger := "schedule"
	if target != "" {
		trigger = "open " + target
	}
	started := time.Now()
	output, ok := executeCollector(childCtx, cmd)
	took := time.Since(started).Round(time.Second)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = want{}
	s := c.states[original.plugin.ID]
	if s == nil || !reflect.DeepEqual(s.plugin, original.plugin) || !s.modified.Equal(original.modified) {
		logEvent(cfg, "run", original.plugin.ID, trigger+" discarded after "+took.String()+": the collector changed or was removed during the run")
		return
	}
	if ctx.Err() != nil {
		logEvent(cfg, "run", original.plugin.ID, trigger+" stopped after "+took.String()+": the app was quitting or the server changed")
		s.state = "idle"
		return
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "ATRACE_ACCOUNT ") {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				s.account = fields[1]
			}
		}
	}
	tail := strings.Join(lines[max(0, len(lines)-10):], " ")
	state := "error"
	switch {
	case ok:
		state = "ok"
	case blockedPattern.MatchString(tail):
		state = "blocked"
	case loginPattern.MatchString(tail):
		state = "login"
	}
	logEvent(cfg, "run", original.plugin.ID, trigger+" "+state+" "+took.String()+" "+runSummary(output, ok))
	if target == "" {
		now := time.Now()
		every := s.schedule.Next(now).Sub(now)
		delay := every
		if ok {
			s.failures = 0
		} else {
			delay = min(time.Duration(900*math.Pow(2, float64(min(s.failures, 7))))*time.Second, every, 24*time.Hour)
			s.failures++
		}
		s.last = now
		s.next = s.last.Add(delay)
	}
	if target == "" || state == "blocked" || state == "login" {
		s.state = state
		if ok {
			tail = ""
			for _, line := range lines {
				if strings.HasPrefix(line, "{") {
					tail = line
				}
			}
		}
		message := []rune(strings.Join(strings.Fields(strings.ReplaceAll(tail, "\x00", " ")), " "))
		s.message = string(message[:min(300, len(message))])
	}
}

func executeCollector(ctx context.Context, cmd *exec.Cmd) (string, bool) {
	var log *os.File
	var err error
	root := ""
	for _, pair := range cmd.Env {
		if strings.HasPrefix(pair, "ATRACE_HOME=") {
			root = strings.TrimPrefix(pair, "ATRACE_HOME=")
			break
		}
	}
	if root != "" {
		log, err = os.OpenFile(filepath.Join(root, "logs", filepath.Base(cmd.Dir)+".log"), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	} else {
		log, err = os.CreateTemp("", "atrace-collector-*.log")
	}
	if err != nil {
		return err.Error(), false
	}
	if root == "" {
		defer os.Remove(log.Name())
	}
	defer log.Close()
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		return err.Error(), false
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var result error
	select {
	case result = <-done:
	case <-ctx.Done():
		// CommandContext sends SIGTERM; always kill the group after the grace period,
		// including helpers that outlive their parent.
		select {
		case result = <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			result = <-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	end, _ := log.Seek(0, io.SeekEnd)
	_, _ = log.Seek(max(0, end-65536), io.SeekStart)
	raw, _ := io.ReadAll(log)
	if ctx.Err() != nil {
		return string(raw) + "\nCollector stopped or timed out", false
	}
	return string(raw), result == nil
}

func collectRequest(ctx context.Context, cfg settings, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, cfg.server+path, body)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+cfg.token())
	}
	return req, err
}
func (c *collectors) report(ctx context.Context, cfg settings) {
	for ctx.Err() == nil {
		if cfg.token() == "" {
			c.connectionChanged("未设置采集令牌")
		} else {
			c.mu.Lock()
			rows := []map[string]string{}
			for _, s := range c.states {
				state := s.state
				if s.plugin.Paused {
					state = "paused"
				}
				row := map[string]string{"name": s.plugin.ID, "state": state, "account": s.account, "message": s.message}
				if !s.last.IsZero() {
					row["lastRun"] = s.last.UTC().Format(time.RFC3339)
				}
				if s.plugin.eligible(s.state) {
					row["nextRun"] = s.next.UTC().Format(time.RFC3339)
				}
				rows = append(rows, row)
			}
			c.mu.Unlock()
			sort.Slice(rows, func(i, j int) bool { return rows[i]["name"] < rows[j]["name"] })
			host, _ := os.Hostname()
			raw, _ := json.Marshal(map[string]any{"device": host, "collectors": rows})
			reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			req, _ := collectRequest(reqCtx, cfg, "POST", "/collect/status", strings.NewReader(string(raw)))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			status := "未连接"
			if err == nil {
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					status = "已连接"
				}
				io.Copy(io.Discard, io.LimitReader(resp.Body, 65536))
				resp.Body.Close()
			}
			cancel()
			if ctx.Err() == nil {
				c.connectionChanged(status)
			}
		}
		if !wait(ctx, 15*time.Second) {
			return
		}
	}
}
func (c *collectors) platforms() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := []string{}
	for _, s := range c.states {
		if s.plugin.eligible(s.state) {
			ids = append(ids, s.plugin.ID)
		}
	}
	slices.Sort(ids)
	return ids
}
func (c *collectors) subscribe(ctx context.Context, cfg settings) {
	for ctx.Err() == nil {
		ids := c.platforms()
		if len(ids) == 0 || cfg.token() == "" {
			if !wait(ctx, 3*time.Second) {
				return
			}
			continue
		}
		subCtx, cancel := context.WithCancel(ctx)
		// Reloaded, paused and removed plugins change the subscription without restarting the App.
		changed := make(chan struct{})
		go func() {
			defer close(changed)
			for wait(subCtx, 3*time.Second) {
				if !slices.Equal(ids, c.platforms()) {
					cancel()
					return
				}
			}
		}()
		req, _ := collectRequest(subCtx, cfg, "GET", "/collect/wants?"+url.Values{"platforms": {strings.Join(ids, ",")}}.Encode(), nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				scan := bufio.NewScanner(resp.Body)
				scan.Buffer(make([]byte, 4096), 65536)
				for scan.Scan() {
					line := scan.Text()
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					var w want
					if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &w) == nil {
						c.enqueue(w)
					}
				}
			}
			resp.Body.Close()
		}
		cancel()
		<-changed
		if !wait(ctx, 10*time.Second) {
			return
		}
	}
}
func (c *collectors) enqueue(w want) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if w.Platform == "" || w.Post == "" || w == c.running || slices.Contains(c.queue, w) || len(c.queue) >= 50 {
		return
	}
	c.queue = append(c.queue, w)
}
