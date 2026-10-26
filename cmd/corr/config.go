package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// config is where corr keeps its server and token: $XDG_CONFIG_HOME/corr
// (or ~/.config/corr) /config.json, readable only by its owner.
// CORR_SERVER and CORR_TOKEN override it, for scripts and agents.
type config struct {
	Server   string `json:"server"`
	Token    string `json:"token"`
	Username string `json:"username,omitempty"`
}

func configPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "corr", "config.json"), nil
}

func loadConfig() (config, error) {
	var c config
	p, err := configPath()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &c); err != nil {
			return c, errors.New(p + " isn't valid JSON")
		}
	}
	if v := os.Getenv("CORR_SERVER"); v != "" {
		c.Server = v
	}
	if v := os.Getenv("CORR_TOKEN"); v != "" {
		c.Token = v
	}
	return c, nil
}

func saveConfig(c config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func removeConfig() error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
