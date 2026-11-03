package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/kingzad/drill/internal/book"
)

// config is the desktop app's small piece of persistent state: which folder of
// books was last opened. It lives outside the books folder so runbook repos
// stay clean.
type config struct {
	Dir string `json:"dir"`
}

func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "drill", "config.json"), nil
}

func loadConfig() config {
	path, err := configPath()
	if err != nil {
		return config{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}
	}
	var c config
	if json.Unmarshal(data, &c) != nil {
		return config{}
	}
	return c
}

func saveConfig(c config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// resolveBooksDir picks the folder to open at startup: DRILL_DIR, then the last
// folder used, then the current directory if it already contains books, and
// finally ~/Drill.
func resolveBooksDir() string {
	if dir := os.Getenv("DRILL_DIR"); dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			return abs
		}
	}
	if dir := loadConfig().Dir; dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	if wd, err := os.Getwd(); err == nil && containsBooks(wd) {
		return wd
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return "."
	}
	dir := filepath.Join(home, "Drill")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// containsBooks reports whether dir holds at least one file that actually loads
// as a Book. A bare *.json check would treat any repository containing a
// wails.json or package.json as a books folder.
func containsBooks(dir string) bool {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(matches) == 0 {
		return false
	}
	if len(matches) > 20 {
		matches = matches[:20]
	}
	for _, path := range matches {
		if _, err := book.Load(path); err == nil {
			return true
		}
	}
	return false
}
