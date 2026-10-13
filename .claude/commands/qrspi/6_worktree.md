---
description: Create an isolated git worktree for implementation
argument-hint: "docs/plans/<id>/"
---

# Worktree — Isolate the Implementation

Create a git worktree so implementation happens on an isolated branch without affecting your main working tree.

## Input

The artifact directory is `$ARGUMENTS`.

## Process

1. **Determine identifiers**:
   - Branch name: `<prefix>/<short-name>` per `CLAUDE.md` — prefix is one of `feat/`, `fix/`, `chore/`, `docs/`, `refactor/`, short-name in kebab-case. Derive the short-name from the artifact directory (e.g. `docs/plans/84-otp-webstart-v2/` → `fix/otp-webstart-v2`). **Ask the user to confirm the prefix** — the issue number does not tell you whether the work is a fix or a feature.
   - Main tree path: the first line of `git worktree list` (needed in step 4, before the session moves into the worktree).
   - Worktree path: `<main-tree>/.claude/worktrees/<prefix>+<short-name>` — `EnterWorktree` turns the `/` in the name into `+`.

2. **Confirm with the user** before executing:
   ```
   Ready to create worktree:

   Worktree: <main-tree>/.claude/worktrees/<prefix>+<short-name>
   Branch: <prefix>/<short-name>
   Plan: $ARGUMENTS/plan.md

   To implement, run from the worktree:
     /qrspi:7_implement $ARGUMENTS

   Proceed?
   ```

3. **Create the worktree** after the user confirms, with the `EnterWorktree` tool in `name` mode. It branches from `origin/main` and moves the session into the worktree; then rename the auto-named branch:
   ```
   EnterWorktree({name: "<prefix>/<short-name>"})
   git branch -m worktree-<prefix>+<short-name> <prefix>/<short-name>
   ```
   This repo has no submodules, so no extra init step is needed.

4. **Copy QRSPI artifacts** into the worktree. `docs/plans/` is gitignored, so these files never appear in a worktree on their own and no commit will ever carry them across:
   ```
   mkdir -p docs/plans
   cp -r <main-tree>/$ARGUMENTS ./$ARGUMENTS
   ```

5. **Install frontend deps and build the frontend once.** Both `frontend/node_modules` and
   `frontend/dist` are gitignored, so a fresh worktree has neither:
   ```
   cd frontend && npm ci && npm run build
   ```
   `npm ci` is what `task dev` needs. `npm run build` is what **`go build ./...`** needs —
   `main.go` embeds `all:frontend/dist`, so without it the build fails with
   `pattern all:frontend/dist: no matching files found` before any code is touched. That
   message means a missing prerequisite, not a regression. `go test ./internal/...` is
   unaffected either way, since it never builds the root package.

6. **Report the worktree path.** The session now runs inside it, so plain `git` commands target the worktree. The isolated session refuses `git -C <main-tree>`; leave with `ExitWorktree({action: "keep"})` before touching the main tree.

## Output

- Git worktree created at `<main-tree>/.claude/worktrees/<prefix>+<short-name>`, session inside it
- QRSPI artifacts copied into the worktree
- Frontend deps installed if needed
- Tell the user the worktree path and how to start implementation

## Rules

- Always confirm before creating the worktree.
- Worktrees do not share untracked files with the main tree. Always copy the artifact directory after creating the worktree.
- Never create the branch on `main` and never commit to `main`.
- **Removing the worktree deletes the artifacts with it.** `docs/plans/` is gitignored, so the worktree's copy is the only one that saw the implementation phases — copy it back to `<main-tree>/docs/plans/` (or somewhere outside the repo) *before* removing the worktree, and leave the worktree with `ExitWorktree({action: "keep"})` first. No commit will do this for you.
- Do not start implementation. That's a separate phase with a separate context window.

## When to Go Back

If the plan doesn't exist yet at `$ARGUMENTS/plan.md`, tell the user to run `/qrspi:5_plan` first.
