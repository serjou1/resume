// Command resume shows the Claude Code sessions started in the current
// directory, the same list as /resume inside Claude Code, and resumes the one
// you pick. The list lives in ./.resume.json.
//
//	resume [claude flags...]  open the menu; extra flags go to `claude --resume`
//	resume "text" [flags...]  find the session containing text and resume it
//	resume hook               SessionStart hook: record a new session
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "hook":
			runHook()
			return
		case "-h", "--help", "help":
			fmt.Println(`usage:
  resume [claude flags...]  menu of Claude Code sessions in this directory
                            (enter: resume, del: delete, type: search, esc: quit)
  resume "text" [flags...]  find sessions whose conversation contains text:
                            one is resumed at once, several open the menu;
                            searches this directory first, then all projects
  resume hook               SessionStart hook, records new sessions in .resume.json`)
			return
		}
	}

	args := os.Args[1:]
	query := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query, args = args[0], args[1:]
		if normalize(query) == "" {
			fail(fmt.Errorf("empty search text"))
		}
	}

	dir, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	var sessions []Session
	err = withStore(dir, func(st *Store) (bool, error) {
		Sync(dir, st)
		st.sort()
		sessions = append(sessions, st.Sessions...)
		return true, nil
	})
	if err != nil {
		fail(err)
	}

	m := newModel(dir, sessions)
	if query != "" {
		found, global := find(dir, sessions, query)
		switch len(found) {
		case 0:
			fmt.Fprintf(os.Stderr, "resume: no sessions contain %q\n", query)
			os.Exit(1)
		case 1:
			fmt.Printf("resuming %s\n", firstNonEmpty(found[0].Title, found[0].ID))
			resume(dir, found[0], global, args)
		}
		m = newModel(dir, found)
		m.global = global
		m.header = fmt.Sprintf("%d sessions contain %q", len(found), truncate(query, 60))
	}
	res, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		fail(err)
	}
	if chosen := res.(*model).chosen; chosen != nil {
		resume(dir, *chosen, m.global, args)
	}
}

// find returns the sessions whose conversation contains query. It searches
// the sessions of dir first; when none of them contains query in Claude's
// part of the conversation, it searches the transcripts of all projects
// (global=true) and keeps the stronger result.
func find(dir string, local []Session, query string) (found []Session, global bool) {
	byPath := map[string]Session{}
	var files []string
	for _, s := range local {
		if s.Messages > 0 {
			byPath[s.Transcript] = s
			files = append(files, s.Transcript)
		}
	}
	paths, strength := Search(files, query)
	if strength < matchSession {
		var others []string
		for _, f := range allTranscripts() {
			if _, ok := byPath[f]; !ok {
				others = append(others, f)
			}
		}
		if gp, gs := Search(others, query); gs > strength {
			paths, global = gp, true
		}
	}

	deleted := map[string]bool{}
	for _, p := range paths {
		s, ok := byPath[p]
		if global {
			fi, err := os.Stat(p)
			if err != nil {
				continue
			}
			s = Session{ID: strings.TrimSuffix(filepath.Base(p), ".jsonl"), Transcript: p, Size: -1}
			parseTranscript(&s, fi)
			if s.Cwd != "" && isDeletedIn(s.Cwd, s.ID, deleted) {
				continue
			}
		} else if !ok {
			continue
		}
		found = append(found, s)
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Updated.After(found[j].Updated) })
	return found, global
}

// isDeletedIn reports whether id is in the deleted list of <cwd>/.resume.json.
// cache holds "cwd\x00id" keys of lists already read.
func isDeletedIn(cwd, id string, cache map[string]bool) bool {
	if _, ok := cache[cwd]; !ok {
		cache[cwd] = true
		var st Store
		if data, err := os.ReadFile(filepath.Join(cwd, storeName)); err == nil && json.Unmarshal(data, &st) == nil {
			for _, d := range st.Deleted {
				cache[cwd+"\x00"+d] = true
			}
		}
	}
	return cache[cwd+"\x00"+id]
}

// resume replaces this process with `claude --resume <id> args...`. A session
// from another project is resumed from the directory it started in, because
// Claude Code looks the session up by the current directory.
func resume(dir string, s Session, global bool, args []string) {
	if global && s.Cwd != "" && s.Cwd != dir {
		if err := os.Chdir(s.Cwd); err != nil {
			fail(err)
		}
		fmt.Printf("cd %s\n", s.Cwd)
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		fail(fmt.Errorf("claude not found in PATH"))
	}
	argv := append([]string{"claude", "--resume", s.ID}, args...)
	fail(syscall.Exec(claude, argv, os.Environ()))
}

// deleteSession removes the session from .resume.json in dir.
func deleteSession(dir, id string) error {
	return withStore(dir, func(st *Store) (bool, error) {
		st.Remove(id)
		return true, nil
	})
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "resume:", err)
	os.Exit(1)
}
