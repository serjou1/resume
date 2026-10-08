package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode"
)

// Match strength is decided by the first record that contains the phrase.
// When that record is a prompt the user typed, the user brought the phrase in
// from somewhere else (for example asked "which session said X"), so the
// session is weaker than one where Claude's answer, thinking or a tool call
// contains the phrase first.
const (
	matchNone = iota
	matchPrompt
	matchSession
)

// normalize lowercases text, drops markdown marks (` * _ ~) and quotes, and
// collapses whitespace, so a phrase copied from the rendered terminal matches
// the raw markdown in the transcript.
func normalize(s string) string {
	var b strings.Builder
	space := true
	for _, r := range s {
		switch {
		case strings.ContainsRune("`*_~\"«»", r):
			continue
		case unicode.IsSpace(r):
			if !space {
				b.WriteByte(' ')
			}
			space = true
		default:
			b.WriteRune(unicode.ToLower(r))
			space = false
		}
	}
	return strings.TrimSpace(b.String())
}

// prefilterWord returns the longest word of the normalized query; a line whose
// lowercased raw JSON lacks it cannot match, so it is not decoded.
func prefilterWord(query string) string {
	best := ""
	for _, w := range strings.Fields(query) {
		if strings.ContainsAny(w, `\/`) {
			continue
		}
		if len(w) > len(best) {
			best = w
		}
	}
	return best
}

// searchTranscript returns how strongly the transcript at path contains query
// (already normalized) in user and assistant messages.
func searchTranscript(path, query string) int {
	f, err := os.Open(path)
	if err != nil {
		return matchNone
	}
	defer f.Close()
	word := []byte(prefilterWord(query))
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && bytes.Contains(bytes.ToLower(line), word) {
			var rec struct {
				Type    string `json:"type"`
				Message *struct {
					Content any `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &rec) == nil && rec.Message != nil &&
				(rec.Type == "user" || rec.Type == "assistant") {
				var parts []string
				collectStrings(rec.Message.Content, &parts)
				if strings.Contains(normalize(strings.Join(parts, " ")), query) {
					if _, typed := rec.Message.Content.(string); typed && rec.Type == "user" {
						return matchPrompt
					}
					return matchSession
				}
			}
		}
		if err != nil {
			return matchNone
		}
	}
}

// collectStrings appends every string value inside v: text, thinking, tool
// inputs and tool results.
func collectStrings(v any, out *[]string) {
	switch v := v.(type) {
	case string:
		*out = append(*out, v)
	case []any:
		for _, e := range v {
			collectStrings(e, out)
		}
	case map[string]any:
		for k, e := range v {
			if k == "signature" || k == "id" || k == "tool_use_id" || k == "type" {
				continue
			}
			collectStrings(e, out)
		}
	}
}

// Search returns the transcripts among files that contain query and the match
// strength. When some transcripts contain it outside user prompts, only those
// are returned.
func Search(files []string, query string) ([]string, int) {
	query = normalize(query)
	strength := make([]int, len(files))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				strength[i] = searchTranscript(files[i], query)
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	best := matchNone
	for _, s := range strength {
		best = max(best, s)
	}
	var found []string
	for i, s := range strength {
		if s != matchNone && s == best {
			found = append(found, files[i])
		}
	}
	return found, best
}

// allTranscripts lists the session transcripts of every Claude Code project.
func allTranscripts() []string {
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(projectDir("/")), "*", "*.jsonl"))
	return files
}
