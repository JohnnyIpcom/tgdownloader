package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultPromptHistoryFilename   = ".tgdownloader_history"
	defaultPromptHistoryMaxEntries = 1000
)

type promptHistoryStore struct {
	path       string
	maxEntries int
	entries    []string
	lastEntry  string
	shouldSkip func(string, []string) bool
}

func newPromptHistoryStore(path string, maxEntries int, shouldSkip func(string, []string) bool) (*promptHistoryStore, error) {
	if maxEntries <= 0 {
		maxEntries = defaultPromptHistoryMaxEntries
	}

	if path == "" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			path = filepath.Join(home, defaultPromptHistoryFilename)
		} else {
			path = defaultPromptHistoryFilename
		}
	}

	entries, err := loadPromptHistory(path)
	if err != nil {
		return nil, err
	}

	store := &promptHistoryStore{
		path:       path,
		maxEntries: maxEntries,
		entries:    entries,
		shouldSkip: shouldSkip,
	}

	if len(store.entries) > store.maxEntries {
		store.entries = store.entries[len(store.entries)-store.maxEntries:]
		if err := store.rewrite(); err != nil {
			return nil, err
		}
	}

	if len(store.entries) > 0 {
		store.lastEntry = store.entries[len(store.entries)-1]
	}

	return store, nil
}

func loadPromptHistory(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}

		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	var entries []string
	for scanner.Scan() {
		entry := strings.TrimSpace(scanner.Text())
		if entry == "" {
			continue
		}

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func (s *promptHistoryStore) Entries() []string {
	entries := make([]string, len(s.entries))
	copy(entries, s.entries)
	return entries
}

func (s *promptHistoryStore) Save(entry string, args []string) error {
	_, err := s.Record(entry, args)
	return err
}

func (s *promptHistoryStore) Record(entry string, args []string) (bool, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" || entry == s.lastEntry {
		return false, nil
	}

	if s.shouldSkip != nil && s.shouldSkip(entry, args) {
		return false, nil
	}

	s.entries = append(s.entries, entry)
	s.lastEntry = entry

	if len(s.entries) > s.maxEntries {
		s.entries = s.entries[len(s.entries)-s.maxEntries:]
		return true, s.rewrite()
	}

	return true, s.append(entry)
}

func (s *promptHistoryStore) append(entry string) error {
	if err := ensureParentDir(s.path); err != nil {
		return err
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(entry + "\n")
	return err
}

func (s *promptHistoryStore) rewrite() error {
	if err := ensureParentDir(s.path); err != nil {
		return err
	}

	tmpPath := s.path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	w := bufio.NewWriter(f)
	for _, entry := range s.entries {
		if _, err := w.WriteString(entry + "\n"); err != nil {
			f.Close()
			return err
		}
	}

	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}

	return os.Rename(tmpPath, s.path)
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}

	return os.MkdirAll(dir, 0o755)
}

func (r *Root) promptHistorySettings() (bool, string, int) {
	enabled := true
	if r.cfg.IsSet("prompt.history.enabled") {
		enabled = r.cfg.GetBool("prompt.history.enabled")
	}

	path := r.cfg.GetString("prompt.history.path")
	maxEntries := r.cfg.GetInt("prompt.history.max_entries")
	if maxEntries <= 0 {
		maxEntries = defaultPromptHistoryMaxEntries
	}

	return enabled, path, maxEntries
}

func (r *Root) shouldSkipPromptHistoryEntry(entry string, args []string) bool {
	if len(args) == 0 {
		return true
	}

	if args[0] == "exit" {
		return true
	}

	if hasSensitivePromptArgs(args) {
		return true
	}
	if hasSensitivePromptText(entry) {
		return true
	}

	return false
}

func hasSensitivePromptText(entry string) bool {
	entry = strings.ToLower(entry)
	for _, token := range []string{
		"password", "passwd", "token", "secret", "apikey", "api-key", "api_key", "authorization", "cookie",
	} {
		if strings.Contains(entry, "--"+token) {
			return true
		}
	}
	return false
}

func hasSensitivePromptArgs(args []string) bool {
	expectsValue := false

	for _, arg := range args {
		if expectsValue {
			return true
		}

		if !strings.HasPrefix(arg, "-") {
			continue
		}

		flag := strings.TrimLeft(arg, "-")
		name, _, hasValue := strings.Cut(flag, "=")
		if !isSensitivePromptToken(name) {
			expectsValue = false
			continue
		}

		if hasValue {
			return true
		}

		expectsValue = true
	}

	return false
}

func isSensitivePromptToken(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))

	for _, token := range []string{
		"password",
		"passwd",
		"pass",
		"token",
		"secret",
		"apikey",
		"api-key",
		"api_key",
		"authorization",
		"cookie",
	} {
		if strings.Contains(name, token) {
			return true
		}
	}

	return false
}
