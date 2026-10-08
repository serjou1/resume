// Command resume shows the Claude Code sessions started in the current
// directory, the same list as /resume inside Claude Code, and resumes the one
// you pick. The list lives in ./.resume.json.
//
//	resume [claude flags...]  open the menu; extra flags go to `claude --resume`
//	resume hook               SessionStart hook: record a new session
package main

import (
	"fmt"
	"os"
	"os/exec"
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
  resume hook               SessionStart hook, records new sessions in .resume.json`)
			return
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
	res, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		fail(err)
	}
	chosen := res.(*model).chosen
	if chosen == nil {
		return
	}

	claude, err := exec.LookPath("claude")
	if err != nil {
		fail(fmt.Errorf("claude not found in PATH"))
	}
	args := append([]string{"claude", "--resume", chosen.ID}, os.Args[1:]...)
	fail(syscall.Exec(claude, args, os.Environ()))
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
