# resume

`resume` opens a menu of the Claude Code sessions started in the current
directory, the same list as `/resume` inside Claude Code, and runs
`claude --resume <id>` for the session you pick.

The list lives in `./.resume.json`. On every start `resume` adds sessions it
finds in `~/.claude/projects/<dir>/*.jsonl`, and the `resume hook`
SessionStart hook adds each new session the moment Claude Code starts. When
the directory is a git repository, `.resume.json` is added to
`.git/info/exclude`.

## Install

```sh
go install github.com/serjou1/resume@latest
```

Record new sessions in `.resume.json` as they start — add to
`~/.claude/settings.json`:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "command": "~/go/bin/resume hook", "timeout": 5 }] }
    ]
  }
}
```

Without the hook `resume` still picks up every session the next time it runs.

## Find a session by text

```sh
resume "Создаю пользователей igor и victor с ролью Viewer в Grafana"
```

`resume "text"` looks for sessions whose conversation contains the text:
prompts, Claude's answers and thinking, tool calls and tool output. Case,
whitespace, quotes and markdown marks (`` ` * _ ~ ``) are ignored, so text
copied from the terminal matches the raw markdown in the transcript.

- one session found: it is resumed at once;
- several found: the menu shows only them;
- none found: `resume: no sessions contain "…"`, exit code 1.

It searches the sessions of the current directory first, then the transcripts
of all projects. A session from another project is resumed from the directory
it started in.

A session where the text first appears in a prompt you typed (for example a
session where you asked "which session said …") ranks below a session where
the text first appears in Claude's part of the conversation, and is shown only
when no such session exists.

## Keys

| key | action |
|---|---|
| ↑ ↓, PgUp PgDn, Home End | select |
| Enter | resume the session |
| Del (fn+⌫ on Mac), or ⌫ with an empty search | delete from history, asks y/n |
| any text | search; every word must match title, first prompt, branch or id |
| Esc | clear the search, then quit |

Extra arguments go to Claude Code: `resume --model opus` runs
`claude --resume <id> --model opus`.

Delete removes the session from `.resume.json` and keeps its id in the
`deleted` list, so it does not come back on the next sync. The transcript in
`~/.claude/projects` stays, and `/resume` inside Claude Code still shows it.

## .resume.json

```json
{
  "sessions": [
    {
      "id": "ae47b73d-…",
      "title": "Init project",
      "first_prompt": "/init",
      "branch": "main",
      "messages": 21,
      "created": "2026-10-01T10:00:00Z",
      "updated": "2026-10-01T10:20:00Z",
      "transcript": "/Users/me/.claude/projects/-Users-me-app/ae47b73d-….jsonl",
      "size": 48213
    }
  ],
  "deleted": ["…"]
}
```

`title` is the `/rename` title, else the AI title Claude Code generates, else
the first prompt. `size` is how many transcript bytes were parsed; the next
run parses only the bytes appended after it.
