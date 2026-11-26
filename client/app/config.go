package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type settings struct {
	home, directory, file, server, bin string
	env                                map[string]string
}

type appConfig struct {
	DevHost            string `json:"devHost,omitempty"`
	Server             string `json:"server"`
	CollectToken       string `json:"collectToken,omitempty"`
	RemoteCollectToken string `json:"remoteCollectToken,omitempty"`
}

func loadSettings() (settings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return settings{}, err
	}
	root := os.Getenv("ATRACE_HOME")
	if root == "" {
		root = filepath.Join(home, ".atrace")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return settings{}, err
	}
	for _, dir := range []string{root, filepath.Join(root, "collectors"), filepath.Join(root, "logs")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return settings{}, err
		}
	}
	cfg := settings{home: home, file: filepath.Join(root, "config.json"), directory: filepath.Join(root, "collectors"), env: map[string]string{}}
	var stored appConfig
	raw, err := os.ReadFile(cfg.file)
	if err == nil {
		if err := json.Unmarshal(raw, &stored); err != nil {
			return settings{}, fmt.Errorf("读取 config.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return settings{}, err
	}

	cfg.env["ATRACE_COLLECT_TOKEN"] = stored.CollectToken
	cfg.env["ATRACE_REMOTE_COLLECT_TOKEN"] = stored.RemoteCollectToken
	for _, pair := range os.Environ() {
		k, v, _ := strings.Cut(pair, "=")
		cfg.env[k] = v
	}
	if directory := cfg.env["ATRACE_COLLECTORS"]; directory != "" {
		cfg.directory = directory
	}
	for _, candidate := range []string{cfg.env["ATRACE_URL"], stored.Server, "http://127.0.0.1:8183"} {
		if address, err := serverAddress(candidate); err == nil {
			cfg.server = address
			break
		}
	}
	if raw == nil {
		stored.Server = cfg.server
		if err := writeConfig(cfg.file, stored); err != nil {
			return settings{}, err
		}
	}
	executable, _ := os.Executable()
	cfg.bin = filepath.Dir(executable)
	cfg.env["ATRACE_HOME"] = root
	return cfg, nil
}

func serverAddress(input string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(input))
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(u.Scheme == "http" && localServer(u))) {
		return "", errors.New("请输入服务器地址，远程服务器需要使用 HTTPS。")
	}
	u.Path = ""
	return u.String(), nil
}

func localServer(u *url.URL) bool {
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func (s settings) token() string {
	u, _ := url.Parse(s.server)
	if localServer(u) {
		return s.env["ATRACE_COLLECT_TOKEN"]
	}
	return s.env["ATRACE_REMOTE_COLLECT_TOKEN"]
}

func (s settings) saveServer(server string) error {
	var stored appConfig
	raw, err := os.ReadFile(s.file)
	if err == nil {
		if err := json.Unmarshal(raw, &stored); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	stored.Server = server
	return writeConfig(s.file, stored)
}

func writeConfig(path string, cfg appConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
