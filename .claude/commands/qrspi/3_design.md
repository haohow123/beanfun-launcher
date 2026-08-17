---
description: Design discussion — align on where we are going before planning how
model: opus
argument-hint: "docs/plans/<id>/"
---

# Design — Where Are We Going?

Create a ~200-line design document that captures the current state, desired end state, design decisions, and patterns to follow. This is the **lowest-cost point for direction changes** — get alignment here before investing in detailed planning.

## Input

Read `$ARGUMENTS/task.md`, `$ARGUMENTS/questions.md`, and `$ARGUMENTS/research.md`.

## Process

1. **Read all three artifacts fully.** `task.md` tells you what we're building. `research.md` tells you what exists. Understand both before proceeding.

2. **Targeted exploration**: If the research revealed areas that need deeper investigation for design decisions, spawn **Explore** agents to examine specific patterns or call paths.

3. **Present open questions and wait for answers.** Before writing anything, you MUST:
   - List 3-5 design questions that require human judgment
   - Present options with trade-offs for each, grounded in what the research found
   - Wait for the user to respond

   Example:
   ```
   Before I write the design document, I need your input:

   **Q1: Where the new decoder lives**
   The research shows two patterns in the codebase:
   - Option A: [pattern from research.md] — used in [file:line], simpler but less flexible
   - Option B: [pattern from research.md] — used in [file:line], more complex but extensible
   Which fits this use case?

   **Q2: ...**
   ```

   Do NOT skip this step. Do NOT write the design document without user input.

   Scan `task.md` and the issue first: anything the issue already specifies is **not** a question. Do not dress up a decision the user already made as a choice.

4. **Security gate.** If the design touches credentials, tokens, secret storage, or outbound network calls, it MUST include a section stating how it complies with the non-negotiable principles in `CLAUDE.md`:
   - which hosts are contacted, and that they are Gamania-owned
   - where secrets live, and that nothing sensitive reaches logs or error strings
   - when tokens are zeroed
   - any new dependency, and why the stdlib is insufficient

   A design that touches these without that section is incomplete.

5. **Write `design.md`** (~200 lines) to the artifact directory:

   ```markdown
   # Design Discussion

   ## Current State
   [What exists today, grounded in research findings with file:line refs]

   ## Desired End State
   [What we're building and how to verify it's correct]

   ## Patterns to Follow
   [Existing codebase patterns the implementation should match, with file:line refs.
   Flag any patterns the research found that should NOT be followed.]

   ## Design Decisions
   1. **[Decision name]**: [chosen option] — [why]
   2. **[Decision name]**: [chosen option] — [why]
   ...

   ## Security Compliance
   [Required when the change touches credentials, secrets, or network. See step 4.]

   ## What We're NOT Doing
   [Explicit scope boundaries to prevent creep]

   ## Open Risks
   [Anything uncertain that might surface during implementation]
   ```

6. **Present the design to the user** for review. Iterate until they approve.

## Output

- File written: `docs/plans/<id>/design.md`
- Tell the user: "Next: run `/qrspi:4_structure docs/plans/<id>/`"

## Rules

- ~200 lines max. This is a steering document, not a specification.
- Every pattern reference must cite `file:line` from the research.
- You MUST ask questions and wait before writing. No exceptions.
- "Patterns to Follow" is critical — call out both good and bad patterns found in the codebase.
- "What We're NOT Doing" prevents scope creep downstream.
- Prefer Go stdlib. A new dependency is a design decision that needs its own justification line.

## When to Go Back

If the research is missing critical information needed for design decisions — the questions missed an important area of the codebase — tell the user and suggest re-running `/qrspi:1_question` and `/qrspi:2_research` to fill the gap before proceeding with an incomplete design.
