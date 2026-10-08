package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const transcript = `{"type":"mode","mode":"normal","sessionId":"s1"}
{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>x</local-command-caveat>"},"timestamp":"2026-10-01T10:00:00Z","gitBranch":"main"}
{"type":"user","message":{"role":"user","content":"<command-name>/init</command-name><command-args>fast</command-args>"},"timestamp":"2026-10-01T10:00:01Z","gitBranch":"main"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]},"timestamp":"2026-10-01T10:00:02Z","gitBranch":"feat"}
{"type":"assistant","isSidechain":true,"message":{"content":[]},"timestamp":"2026-10-01T10:00:03Z"}
{"type":"ai-title","aiTitle":"Init project","sessionId":"s1"}
`

func setup(t *testing.T) (cwd, proj string) {
	cfg := t.TempDir()
	cwd = t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	proj = projectDir(cwd)
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
	return cwd, proj
}

func TestProjectDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/c")
	if got := projectDir("/Users/serjou/.claude-adhd"); got != "/c/projects/-Users-serjou--claude-adhd" {
		t.Fatal(got)
	}
}

func TestSyncParsesTranscript(t *testing.T) {
	cwd, _ := setup(t)
	st := &Store{}
	Sync(cwd, st)
	if len(st.Sessions) != 1 {
		t.Fatalf("sessions: %+v", st.Sessions)
	}
	s := st.Sessions[0]
	if s.Title != "Init project" || s.FirstPrompt != "/init fast" || s.Messages != 2 || s.Branch != "feat" {
		t.Fatalf("parsed: %+v", s)
	}
	if s.Updated.Format("15:04:05") != "10:00:03" {
		t.Fatalf("updated: %v", s.Updated)
	}
}

func TestDeleteSurvivesSync(t *testing.T) {
	cwd, _ := setup(t)
	if err := withStore(cwd, func(st *Store) (bool, error) { Sync(cwd, st); return true, nil }); err != nil {
		t.Fatal(err)
	}
	if err := deleteSession(cwd, "s1"); err != nil {
		t.Fatal(err)
	}
	withStore(cwd, func(st *Store) (bool, error) {
		Sync(cwd, st)
		if len(st.Sessions) != 0 || !st.isDeleted("s1") {
			t.Fatalf("after delete: %+v", st)
		}
		return false, nil
	})
}

func TestHookRecordsSession(t *testing.T) {
	cwd, proj := setup(t)
	r, w, _ := os.Pipe()
	w.WriteString(`{"session_id":"s1","transcript_path":"` + filepath.Join(proj, "s1.jsonl") + `","cwd":"` + cwd + `","source":"startup"}`)
	w.Close()
	old := os.Stdin
	os.Stdin = r
	runHook()
	os.Stdin = old
	data, err := os.ReadFile(filepath.Join(cwd, storeName))
	if err != nil || !strings.Contains(string(data), `"id": "s1"`) {
		t.Fatalf("store: %s %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(cwd, storeName+".lock")); err == nil {
		t.Fatal("lock file left behind")
	}
}

func TestIncrementalParse(t *testing.T) {
	cwd, proj := setup(t)
	st := &Store{}
	Sync(cwd, st)
	f, _ := os.OpenFile(filepath.Join(proj, "s1.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"user","message":{"content":"next"},"timestamp":"2026-10-02T10:00:00Z","gitBranch":"dev"}` + "\n" + `{"type":"custom-title","customTitle":"Renamed"}` + "\n" + `{"type":"ai-title","aiTitle":"Later AI"}` + "\n" + `{"type":"user","message":{"content":"half`)
	f.Close()
	Sync(cwd, st)
	s := st.Sessions[0]
	if s.Title != "Renamed" || s.FirstPrompt != "/init fast" || s.Messages != 3 || s.Branch != "dev" || s.Updated.Day() != 2 {
		t.Fatalf("incremental: %+v", s)
	}
	fi, _ := os.Stat(filepath.Join(proj, "s1.jsonl"))
	if s.Size >= fi.Size() {
		t.Fatalf("partial last line was consumed: size %d of %d", s.Size, fi.Size())
	}
}
