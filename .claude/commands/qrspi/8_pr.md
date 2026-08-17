---
description: Create a pull request with context from the design discussion
argument-hint: "docs/plans/<id>/"
---

# PR — Create the Pull Request

Create a pull request with a description grounded in the design document and the actual diff.

## Input

Read `$ARGUMENTS/design.md` for context on what was built and why, and `$ARGUMENTS/task.md` for the issue it closes.

## Process

1. **Gather PR information:**
   - `git diff main...HEAD` — the branch's own changes (three dots: two dots would count commits others merged into `main` as deletions)
   - `git log main...HEAD --oneline` — commit history
   - Read `$ARGUMENTS/design.md` for the "why" behind the changes

2. **Re-run the full verification suite** before opening the PR, and re-run it again after any rebase. Numbers tied to repo state (test counts, line counts) go stale the moment `main` moves:
   ```
   gofmt -l . ; golangci-lint run ; go build ./... ; go test ./internal/...
   ```

3. **Push the branch** if it isn't pushed yet: `git push -u origin <branch>`.

4. **Create the PR** using `gh pr create --base main`:

   ```
   gh pr create --base main --title "<concise imperative title under 70 chars>" --body "$(cat <<'EOF'
   ## Summary
   [2-3 bullets: what this PR does and why, drawn from design.md]

   ## Design Decisions
   [Key decisions from design.md that reviewers should understand]

   ## Changes
   [Brief description of what changed, organized by component]

   ## How to Verify
   - [ ] [Automated verification command]
   - [ ] [Manual verification step]

   ## References
   - Design: `$ARGUMENTS/design.md`
   - Plan: `$ARGUMENTS/plan.md`

   Closes #<issue-number>

   🤖 Generated with [Claude Code](https://claude.com/claude-code)
   EOF
   )"
   ```

5. **Report the PR URL** to the user and stop.

## Output

- PR opened against `main`
- URL reported to the user

## Rules

- Title under 70 chars, imperative, single line. Use the body for details.
- The summary should explain WHY, not just WHAT. The diff shows what changed; the PR description should explain the reasoning.
- Every concrete number in the body (line counts, test counts, `file:line` refs) must be re-derived at write time, not carried over from an earlier message.
- `Closes #<issue>` so the issue closes on merge. Omit only if the PR is a partial step.
- The PR body ends with the attribution footer above. No `Co-Authored-By` in PR bodies — that belongs in commit messages only.
- If a PR already exists for this branch, update it with `gh pr edit` instead of creating a new one.

## Merging — not your call

**Never merge.** Open the PR, summarize it, and wait for the user's explicit "ok merge". Never use `gh pr merge --auto`.

After the user says "ok merge":
- `gh pr merge --squash --delete-branch`
- `main` branch protection requires an approving review the solo owner cannot give. If the merge fails with `the base branch policy prohibits the merge`, retry with `--admin` appended. The user's "ok merge" is the authorization for that bypass — do not ask twice.
- Then sync: `git switch main && git pull --ff-only && git branch -d <branch>`
- If the work used a worktree, copy back anything newer than the main tree before `git worktree remove`.
