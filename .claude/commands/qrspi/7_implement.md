---
description: Execute the plan phase by phase with verification checkpoints
argument-hint: "docs/plans/<id>/"
---

# Implement — Execute the Plan

Implement the plan one phase at a time, verifying each phase before proceeding. Update the plan's checkboxes as you go — they are your progress tracker and context-recovery mechanism.

## Input

Read `$ARGUMENTS/plan.md`. That is your primary working document.

## Process

1. **Read `plan.md` fully.** Check for existing checkmarks (`- [x]`) — if some phases are already complete, pick up from the first unchecked item.

2. **Confirm you are not on `main`**: `git status --porcelain --branch`. If you are, stop and run `/qrspi:6_worktree` or create a branch first. Never commit to `main`.

3. **Read all files referenced in the current phase** before making changes. Understand the code you're modifying.

4. **Implement one phase at a time:**
   - Make the changes described in the plan
   - Follow the plan's intent, but adapt if the codebase has diverged from what the plan expected
   - If you hit a mismatch, stop and present it:
     ```
     Issue in Phase [N]:
     Expected: [what the plan says]
     Found: [actual situation]
     Impact: [what this means for the plan]

     How should I proceed?
     ```

5. **After completing a phase, run verification:**
   - Execute the automated verification commands from the plan
   - Run each as `cmd > /tmp/x.log 2>&1; echo "exit: $?"` so the exit code is the command's own, then read the log. A trailing `| tail` or `; echo` replaces the exit code you care about.
   - Fix any failures before proceeding
   - Check off automated items in `plan.md` using Edit: `- [ ]` becomes `- [x]`

6. **Independent verification — do not self-certify.** Spawn a **verifier** agent with the phase's acceptance criteria as an explicit checklist and neutral wording ("check each condition, report pass/fail with evidence"). It re-reads the files and re-runs the commands in a fresh context. A phase is not done because the agent that wrote it says so.

7. **Security review when applicable.** If the phase touched authentication, secrets, tokens, or outbound network calls, spawn the **security-reviewer** agent over the changed files. This is a non-negotiable in `CLAUDE.md`, not an optional extra.

8. **Prepare the commit** after verification passes:
   - Stage the phase's files: `git add <paths>`
   - Show `git status` and `git diff --cached`
   - Propose the commit message:
     ```
     <imperative summary>

     🤖 Generated with [Claude Code](https://claude.com/claude-code)

     Co-Authored-By: Claude <model> <noreply@anthropic.com>
     ```
   - **Wait for the user's go-ahead before committing.** Once given, you may chain commit → next phase without asking again per phase, unless the diff shape changes materially.
   - One commit per phase so a later phase can be reverted independently. Label work increments **"Milestone N"**, never "Day N".

9. **Pause for manual verification** (unless told to continue through multiple phases):
   ```
   Phase [N] complete — ready for manual verification.

   Automated checks passed:
   - [x] [list what passed]

   Please verify manually:
   - [ ] [manual items from the plan]

   Let me know when done, and I'll proceed to Phase [N+1].
   ```

10. **Repeat** for each phase until the plan is complete.

## Resuming After Context Reset

If you're starting fresh in a new context window:
- Read `plan.md` — checked boxes show what's done
- Trust completed work unless something seems off
- Pick up from the first unchecked item

## Output

- Code changes implemented according to the plan
- `plan.md` updated with checked verification items
- Tell the user: "Next: run `/qrspi:8_pr docs/plans/<id>/`"

## Rules

- One phase at a time. Do not skip ahead.
- Read before you write. Understand existing code before changing it.
- Update checkboxes as you go — they are the source of truth for progress.
- Do not check off manual verification items until the user confirms.
- If the plan has errors, stop and ask. Do not silently deviate.
- Only make changes described in the plan. Do not refactor, clean up, or "improve" code you encounter along the way — even if it's messy. If you see something worth fixing, note it for the user after the phase is done.
- Code comments: English, one sentence maximum, and **default to none** — write one only if the next person changing that line would get it wrong without it. No ticket or issue numbers in comments. No nested ternaries; extract a helper with early returns instead.
- Never log or embed a token, OTP, session key, or any credential-bearing blob in a log line, error string, or test fixture.
- Windows-only phases cannot be verified on the macOS dev machine. Say the verification is deferred; do not claim a check passed when it did not run.
- Use sub-agents for targeted debugging, exploring unfamiliar code, verification, and security review — not for the implementation edits themselves.

## When to Go Back

If a phase reveals the plan is fundamentally wrong — not a small mismatch but a structural issue like a missing dependency, wrong API, or incorrect assumption about the codebase — tell the user. For small mismatches, adapt and continue. For fundamental issues, suggest re-running `/qrspi:5_plan` or even `/qrspi:3_design` with the new information rather than building on a broken foundation.
