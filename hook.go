package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// runHook handles the Claude Code SessionStart hook: it records the new
// session in <cwd>/.resume.json. It never fails the hook and prints nothing
// to stdout, so Claude Code starts the same way with or without it.
func runHook() {
	var in struct {
		SessionID      string `json:"session_id"`
		TranscriptPath string `json:"transcript_path"`
		Cwd            string `json:"cwd"`
	}
	data, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if json.Unmarshal(data, &in) != nil || in.SessionID == "" || in.Cwd == "" {
		return
	}
	err := withStore(in.Cwd, func(st *Store) (bool, error) {
		if st.find(in.SessionID) >= 0 || st.isDeleted(in.SessionID) {
			return false, nil
		}
		now := time.Now().UTC()
		st.Sessions = append(st.Sessions, Session{
			ID:         in.SessionID,
			Transcript: in.TranscriptPath,
			Created:    now,
			Updated:    now,
			Size:       -1,
		})
		return true, nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "resume hook:", err)
	}
}
