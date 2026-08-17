---
description: Decompose a task into neutral research questions
model: opus
argument-hint: "<GitHub issue number/URL, or task description>"
---

# Question — Decompose the Task

Transform a task description into 3-7 specific, neutral research questions. These questions drive the next phase (Research) which runs in a **separate context with no knowledge of what is being built**.

## Input

The user provides a task description, a GitHub issue number (`#84`), or an issue URL.

If given an issue reference, read it first: `gh issue view <number> --comments`. Issue comments often carry the diagnosis that motivated the work.

## Process

1. **Read any provided files and issues fully** before doing anything else.

2. **Light codebase exploration**: Spawn an **Explore** agent (read-only, breadth `medium`) to find which areas of the codebase relate to the task. You need to know what exists to write good questions.

3. **Decompose into 3-7 research questions**:
   - Each question should cause a researcher to explore a different relevant area of the codebase
   - Questions must be **neutral** — they ask what exists and how it works, never how to build something
   - Prefer "trace the flow" questions that reveal architecture over yes/no questions

   Good: "How does the Beanfun client build and send authenticated requests, and where are endpoint hosts defined?"
   Bad: "What's the best way to add a new POST endpoint to the OTP flow?"

   Good: "What patterns exist for table-driven tests of HTTP calls, and how are servers faked?"
   Bad: "How should we test the new v2 endpoint?"

4. **Determine the artifact directory**:
   - With a GitHub issue: `docs/plans/<issue-number>-brief-description/` (e.g. `docs/plans/84-otp-webstart-v2/`)
   - Without an issue: `docs/plans/YYYY-MM-DD-brief-description/`

5. **Create the artifact directory** if it doesn't exist (e.g., `mkdir -p docs/plans/<id>/`).

6. **Write `task.md`** — a clean 2-3 sentence description of what's being built and why, plus a link to the issue if there is one. This file persists the task context for later phases so the user doesn't have to re-explain it.

7. **Write `questions.md`** to the artifact directory:

   ```markdown
   # Research Questions

   ## Context
   [2-3 sentences describing which areas of the codebase to focus on.
   Do NOT mention what is being built or why.]

   ## Questions
   1. [Neutral, fact-seeking question]
   2. [Neutral, fact-seeking question]
   ...
   ```

8. **Present questions to the user** and wait for approval or edits before finalizing.

## Output

- Directory created: `docs/plans/<id>/`
- Files written: `docs/plans/<id>/task.md` and `docs/plans/<id>/questions.md`
- Tell the user: "Next: run `/qrspi:2_research docs/plans/<id>/`"

## Rules

- `questions.md` must NOT contain the task description, goals, or desired behavior
- `task.md` is a brief, honest description of the goal — it will be read by later phases but NOT by Research
- The researcher who reads these questions should have no idea what feature is being built
- Each question should target a different area or concern
- If the task is too simple for 3 questions, tell the user — QRSPI is for complex tasks
- `docs/plans/` is gitignored: these artifacts stay out of version control permanently. They are process notes rather than product, and a capture taken while debugging can carry account details. Never `git add -f` them, and never paste their contents into a PR description or issue without checking what the debugging captured.
- Because they are never tracked, nothing protects them from a `git clean` or a worktree removal. Keep a copy outside the repo for anything you would mind losing.
