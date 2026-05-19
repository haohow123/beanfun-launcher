# beanfun-launcher

A personal third-party launcher for Beanfun (遊戲橘子) games, rewriting existing community launchers because I want full control over what touches my game credentials.

## Why this project exists

Existing community launchers (e.g., pungin/Beanfun) work, but they require trusting someone else's code with my Gamania account login. This project is a from-scratch rewrite where I audit every line and own every dependency.

## Tech stack

- Wails v3 (alpha) — desktop shell, uses native WebView2 on Windows
- Go — backend for HTTP calls to Beanfun API, game launching, Win32 DLL injection
- React + TypeScript + Vite — frontend
- Tailwind + shadcn/ui — UI library (to be added later)
- Locale_Remulator — pre-built C++ DLLs (LRHookx32.dll, LRHookx64.dll) for region emulation. We integrate via injection; we don't author the DLL code.

## Platforms

- Dev: macOS
- Target deploy: Windows 10/11

## Non-negotiable security principles

These are the reason this project exists. Never violate:

1. Never store passwords in plain text. Use OS keyring (macOS Keychain / Windows DPAPI) via a vetted library.
2. Network requests ONLY to Gamania-owned endpoints. No telemetry, analytics, or "phone home" endpoints.
    - HTTP domains: `bfweb.gamania.com`, `tw.beanfun.com`, `tw-event.beanfun.com`, `tw.hicdn.beanfun.com`, `tw.newlogin.beanfun.com`, etc.
    - Game-server TCP probe: connections to `202.80.104.24-29` on port `8484` are permitted **for status detection only** (`internal/maple/status.go`). These IPs are Gamania-owned MapleStory login servers (same list the TMSBug_v2 Discord bot tracks); the launcher sends TCP SYN, on success closes immediately, no application data transmitted. Update the IP list in `gameServerHosts` if Gamania rotates them.
3. Clear sensitive data from memory as soon as possible. Tokens used to launch a game are zeroed after launch.
4. No third-party login providers. Direct to Gamania only.
5. Prefer Go standard library and small focused packages. Reject dependencies that bundle unnecessary network capability.
6. No auto-updater that pulls binaries without signature checks.

## File structure (target)

````
beanfun-launcher/
├── main.go              # Wails v3 app entry
├── frontend/            # React + Vite
├── internal/
│   ├── beanfun/         # Gamania API client
│   ├── launcher/        # Game process launching + DLL injection
│   ├── secrets/         # Keychain/DPAPI wrappers
│   └── locale/          # Locale_Remulator integration
├── build/               # Wails build outputs
└── docs/                # Architecture, threat model, ADRs
````

## Development workflow

Use `task` (go-task) as the canonical entry point — it wraps `wails3` with this project's `build/config.yml` and a pinned Vite port (9245). Bare `wails3 dev` works as a fallback but skips both.

- `task dev` — run with hot reload
- `task build` — build for the current OS
- `task windows:build` — cross-compile Windows binary (the deploy target)
- All Go code passes `gofmt` and `golangci-lint run`
- Tests are table-driven; use `httptest` for HTTP, mocks for keyring

## Git workflow

When making changes, Claude Code should:

1. Make working-tree changes.
2. Stage relevant files (`git add <paths>`).
3. Run `git status` and `git diff --cached`, show me the diff (per atomic commit).
4. Propose commit message(s), splitting into atomic commits when logically separate.
5. Chain through: commit → next commit → ... → push → open PR in one continuous sequence, showing each commit's diff stat + message for visibility but not gating on a response between steps.
6. Stop and wait only at PR open — for CI + my explicit "ok merge". The "ok merge" gate is non-negotiable; never `gh pr merge --auto`.

Never push to `main` directly.

## Branching workflow

This repo follows GitHub Flow. All changes — features, fixes, docs, chores — go through a feature branch and PR, never directly to `main`.

When starting a task:

1. Sync local main: `git switch main && git pull --ff-only`
2. Branch off main: `git switch -c <prefix>/<short-name>`
   - Prefixes: `feat/`, `fix/`, `chore/`, `docs/`, `refactor/`
   - Short-name in kebab-case (e.g. `feat/beanfun-login`, `fix/keychain-error-handling`)
3. Do the work; commits follow the Git workflow above.
4. Push with upstream tracking: `git push -u origin <branch>`
5. Open PR: `gh pr create --base main --head <branch> --title "..." --body "..."`. Title imperative, single line; body explains the why.
6. Wait for the human to review and merge — Claude Code never merges PRs.

After merge:

7. `git switch main && git pull --ff-only`
8. `git branch -d <merged-branch>` (origin's copy is usually auto-deleted by GitHub)

Rules:

- Never commit directly to `main`.
- Never force-push to any branch shared with origin.
- One PR = one logical change. Split unrelated work into separate branches/PRs.

## Subagents

Three custom subagents live in `.claude/agents/`:

- **security-reviewer** — read-only audit of credential and network code. Invoke after any change to auth, secrets, or network logic.
- **go-win32-specialist** — Win32 syscall and DLL injection expert. Invoke when working on `internal/launcher` or `internal/locale`.
- **test-writer** — table-driven Go test generator. Invoke when adding test coverage.

## Out of scope

- Cross-platform support (macOS is dev-only; Linux not targeted)
- Auto-update from internet
- Game patches or mods (launch only)
