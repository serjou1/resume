package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// storeName is the file in the project directory that holds the session list.
const storeName = ".resume.json"

// Session is one Claude Code session started in the project directory.
type Session struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	CustomTitle string    `json:"custom_title,omitempty"` // set by /rename
	AiTitle     string    `json:"ai_title,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	FirstPrompt string    `json:"first_prompt,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	Messages    int       `json:"messages"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
	Transcript  string    `json:"transcript"`
	// Size is the transcript size at the last parse. Claude Code only appends
	// to transcripts, so sync parses just the bytes after Size.
	Size int64 `json:"size"`
}

// Store is the content of .resume.json.
type Store struct {
	Sessions []Session `json:"sessions"`
	// Deleted holds ids removed from the menu, so sync does not add them back.
	Deleted []string `json:"deleted,omitempty"`
}

func (s *Store) find(id string) int {
	for i := range s.Sessions {
		if s.Sessions[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) isDeleted(id string) bool {
	for _, d := range s.Deleted {
		if d == id {
			return true
		}
	}
	return false
}

// Remove drops the session from the list and remembers its id in Deleted.
func (s *Store) Remove(id string) {
	if i := s.find(id); i >= 0 {
		s.Sessions = append(s.Sessions[:i], s.Sessions[i+1:]...)
	}
	if !s.isDeleted(id) {
		s.Deleted = append(s.Deleted, id)
	}
}

func (s *Store) sort() {
	sort.SliceStable(s.Sessions, func(i, j int) bool {
		return s.Sessions[i].Updated.After(s.Sessions[j].Updated)
	})
}

// withStore locks dir, loads <dir>/.resume.json, calls fn and saves the result
// when fn returns save=true. The lock (flock on the directory itself, so no
// lock file is left behind) serialises the menu and the SessionStart hooks of
// parallel Claude sessions.
func withStore(dir string, fn func(*Store) (save bool, err error)) error {
	path := filepath.Join(dir, storeName)
	lock, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}

	st := &Store{}
	created := false
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		created = true
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(data, st); err != nil {
			return err
		}
	}

	save, err := fn(st)
	if err != nil || !save {
		return err
	}
	st.sort()
	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if created {
		excludeFromGit(dir)
	}
	return nil
}

// excludeFromGit adds .resume.json to .git/info/exclude when dir is inside a
// git repository, so the file does not show up in git status.
func excludeFromGit(dir string) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude").Output()
	if err != nil {
		return
	}
	exclude := strings.TrimSpace(string(out))
	data, _ := os.ReadFile(exclude)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == storeName || strings.TrimSpace(line) == "/"+storeName {
			return
		}
	}
	os.MkdirAll(filepath.Dir(exclude), 0o755)
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		f.WriteString("\n")
	}
	f.WriteString(storeName + "\n")
}
