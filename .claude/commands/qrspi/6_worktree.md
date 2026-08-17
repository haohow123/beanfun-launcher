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
   - Repo name: `basename $(git rev-parse --show-toplevel)`
   - Worktree path: `~/wt/<repo-name>/<short-name>`

2. **Confirm with the user** before executing:
   ```
   Ready to create worktree:

   Worktree: ~/wt/<repo-name>/<short-name>
   Branch: <prefix>/<short-name>
   Plan: $ARGUMENTS/plan.md

   To implement, run from the worktree:
     /qrspi:7_implement $ARGUMENTS

   Proceed?
   ```

3. **Create the worktree** after the user confirms:
   ```
   git worktree add ~/wt/<repo-name>/<short-name> -b <prefix>/<short-name>
   ```
   This repo has no submodules, so no extra init step is needed.

4. **Copy QRSPI artifacts** to the worktree. They are untracked until the first commit, and untracked files from the main tree do not appear in worktrees:
   ```
   mkdir -p ~/wt/<repo-name>/<short-name>/docs/plans
   cp -r $ARGUMENTS ~/wt/<repo-name>/<short-name>/$ARGUMENTS
   ```

5. **Install frontend deps in the worktree.** `frontend/node_modules` is gitignored, so a fresh worktree has none and `task dev` will fail without this:
   ```
   cd ~/wt/<repo-name>/<short-name>/frontend && npm ci
   ```
   Skip only if the phases in `plan.md` are Go-only and never run the app.

6. **Report the worktree path** and remind the user that git commands inside it must use `git -C <worktree-path>` (or be run from that directory) — the shell working directory does not persist between tool calls.

## Output

- Git worktree created at `~/wt/<repo-name>/<short-name>`
- QRSPI artifacts copied into the worktree
- Frontend deps installed if needed
- Tell the user the worktree path and how to start implementation

## Rules

- Always confirm before creating the worktree.
- Worktrees do not share untracked files with the main tree. Always copy the artifact directory after creating the worktree.
- Never create the branch on `main` and never commit to `main`.
- When the work is finished, copy back anything in the worktree that is newer than the main tree before `git worktree remove` — removal deletes untracked files with it.
- Do not start implementation. That's a separate phase with a separate context window.

## When to Go Back

If the plan doesn't exist yet at `$ARGUMENTS/plan.md`, tell the user to run `/qrspi:5_plan` first.
