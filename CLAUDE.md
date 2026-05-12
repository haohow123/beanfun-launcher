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
2. Network requests ONLY to Gamania-owned domains (`bfweb.gamania.com`, `tw.beanfun.com`, etc.). No telemetry, analytics, or "phone home" endpoints.
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

- `wails3 dev` — run with hot reload
- `wails3 build` — produce Windows binary
- All Go code passes `gofmt` and `golangci-lint run`
- Tests are table-driven; use `httptest` for HTTP, mocks for keyring

## Subagents

Three custom subagents live in `.claude/agents/`:

- **security-reviewer** — read-only audit of credential and network code. Invoke after any change to auth, secrets, or network logic.
- **go-win32-specialist** — Win32 syscall and DLL injection expert. Invoke when working on `internal/launcher` or `internal/locale`.
- **test-writer** — table-driven Go test generator. Invoke when adding test coverage.

## Out of scope

- Cross-platform support (macOS is dev-only; Linux not targeted)
- Auto-update from internet
- Game patches or mods (launch only)
