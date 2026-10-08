package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// projectDir returns ~/.claude/projects/<dir with every non-alphanumeric
// character replaced by '-'>, the folder where Claude Code keeps transcripts
// of sessions started in dir.
func projectDir(dir string) string {
	home, _ := os.UserHomeDir()
	base := os.Getenv("CLAUDE_CONFIG_DIR")
	if base == "" {
		base = filepath.Join(home, ".claude")
	}
	enc := []byte(dir)
	for i, c := range enc {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			enc[i] = '-'
		}
	}
	return filepath.Join(base, "projects", string(enc))
}

// Sync adds sessions found in the Claude Code project folder to the store,
// re-parses transcripts that grew, and drops sessions whose transcript is gone
// (Claude Code cannot resume them).
func Sync(dir string, st *Store) {
	files, _ := filepath.Glob(filepath.Join(projectDir(dir), "*.jsonl"))
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		if st.isDeleted(id) || st.find(id) >= 0 {
			continue
		}
		st.Sessions = append(st.Sessions, Session{ID: id, Transcript: f, Size: -1})
	}

	kept := st.Sessions[:0]
	for _, s := range st.Sessions {
		if s.Transcript == "" {
			s.Transcript = filepath.Join(projectDir(dir), s.ID+".jsonl")
		}
		fi, err := os.Stat(s.Transcript)
		if err != nil {
			continue
		}
		if fi.Size() != s.Size {
			parseTranscript(&s, fi)
		}
		kept = append(kept, s)
	}
	st.Sessions = kept
}

type record struct {
	Type        string `json:"type"`
	Timestamp   string `json:"timestamp"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	GitBranch   string `json:"gitBranch"`
	AiTitle     string `json:"aiTitle"`
	CustomTitle string `json:"customTitle"`
	Summary     string `json:"summary"`
	Message     *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// parseTranscript fills title, first prompt, branch, message count and times
// of s from its .jsonl transcript. When the transcript grew since the last
// parse, it reads only the appended bytes; when it shrank, it starts over.
func parseTranscript(s *Session, fi os.FileInfo) {
	f, err := os.Open(s.Transcript)
	if err != nil {
		return
	}
	defer f.Close()

	if s.Size > 0 && fi.Size() > s.Size {
		if _, err := f.Seek(s.Size, io.SeekStart); err != nil {
			return
		}
	} else {
		s.CustomTitle, s.AiTitle, s.Summary, s.FirstPrompt, s.Branch, s.Messages = "", "", "", "", "", 0
	}
	custom, ai, summary, first, branch := s.CustomTitle, s.AiTitle, s.Summary, s.FirstPrompt, s.Branch
	created, updated := s.Created, s.Updated
	msgs := s.Messages
	r := bufio.NewReaderSize(f, 1<<20)
	read := s.Size
	if read < 0 || fi.Size() <= s.Size {
		read = 0
	}
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && len(line) > 0 {
			// The last line is still being written; parse it next time.
			break
		}
		read += int64(len(line))
		if len(bytes.TrimSpace(line)) > 0 {
			var rec record
			if json.Unmarshal(line, &rec) == nil {
				if t, e := time.Parse(time.RFC3339Nano, rec.Timestamp); e == nil {
					if created.IsZero() || t.Before(created) {
						created = t
					}
					if t.After(updated) {
						updated = t
					}
				}
				switch rec.Type {
				case "custom-title":
					custom = rec.CustomTitle
				case "ai-title":
					ai = rec.AiTitle
				case "summary":
					summary = rec.Summary
				case "user", "assistant":
					if rec.IsSidechain || rec.IsMeta {
						break
					}
					msgs++
					if rec.GitBranch != "" {
						branch = rec.GitBranch
					}
					if first == "" && rec.Type == "user" && rec.Message != nil {
						first = promptText(rec.Message.Content)
					}
				}
			}
		}
		if err != nil {
			break
		}
	}

	s.CustomTitle, s.AiTitle, s.Summary, s.FirstPrompt = custom, ai, summary, first
	s.Title = firstNonEmpty(custom, ai, summary, first)
	s.Branch = branch
	s.Messages = msgs
	s.Created = created
	if updated.IsZero() {
		updated = fi.ModTime()
	}
	s.Updated = updated
	s.Size = read
}

var (
	commandName = regexp.MustCompile(`<command-name>([^<]*)</command-name>`)
	commandArgs = regexp.MustCompile(`<command-args>([^<]*)</command-args>`)
	spaces      = regexp.MustCompile(`\s+`)
)

// promptText returns the text the user typed, or "" for tool results,
// command output and other records that are not a prompt.
func promptText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &blocks) != nil {
			return ""
		}
		for _, b := range blocks {
			if b.Type == "text" {
				text = b.Text
				break
			}
		}
	}
	text = strings.TrimSpace(text)
	if m := commandName.FindStringSubmatch(text); m != nil {
		cmd := m[1]
		if a := commandArgs.FindStringSubmatch(text); a != nil {
			cmd += " " + a[1]
		}
		text = strings.TrimSpace(cmd)
	}
	if strings.HasPrefix(text, "<") || strings.HasPrefix(text, "Caveat:") || strings.HasPrefix(text, "[Request interrupted") {
		return ""
	}
	return spaces.ReplaceAllString(text, " ")
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
