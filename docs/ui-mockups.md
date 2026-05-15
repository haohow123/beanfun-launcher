# UI mockups

Source of truth for the front-end layout before code lands. Add a new
section whenever a non-trivial UI change is in flight — mermaid
diagram first, then implement.

The diagrams own *shape* (layout, hierarchy, which controls live
next to which). Tailwind class names and pixel-exact sizes live in
the code; this file owns the intent so a reviewer doesn't need to
run the app to follow a UI PR.

---

## HomePage — account card

Status pill + primary CTA on a flat row; no avatar block.

Earlier iterations briefly experimented with per-account coloured
avatars but the unit was wrong: every `(sname, sid)` row belongs to
the same Beanfun user (game-side sub-accounts), so giving each one
a distinct identity colour was misleading. Stripped back to plain
typography for the row; user-level / game-level branding will land
on the card *header* if/when we surface them.

```mermaid
flowchart TB
    subgraph card [Account card — rounded-lg border, hover bg-muted/40]
        direction TB
        subgraph header [Header row — flex items-start justify-between gap-3]
            direction LR
            namestack["sname (semibold)<br/>sid (muted xs code)"]
            pill["status pill<br/>top-right<br/>state-coloured /15 bg"]
        end
        hint["(optional) muted hint<br/>'若沒帶入,點欄位再試一次'<br/>shows after launchStatus=success"]
        err["(optional) destructive text<br/>raw error message<br/>shows after launchStatus=error"]
        fallback["(optional) fallback OTP block<br/>code + 複製 button"]
        actions["Action row — justify-end gap-2<br/>[複製帳密 outline] [帶入帳密 SOLID primary]"]
    end
```

### Status pill colour map

| State        | Label        | Tailwind                                                   |
|--------------|--------------|------------------------------------------------------------|
| `pending`    | 帶入中…       | `bg-blue-500/15 text-blue-700 dark:text-blue-300`           |
| `success`    | ✓ 已送出      | `bg-emerald-500/15 text-emerald-700 dark:text-emerald-300`  |
| `fallback`   | 手動貼上      | `bg-amber-500/15 text-amber-700 dark:text-amber-300`        |
| `no-window`  | 請先按啟動    | `bg-muted text-muted-foreground`                            |
| `error`      | 失敗         | `bg-destructive/15 text-destructive`                        |

---

## HomePage — overall card layout (Pass 3)

```mermaid
flowchart TB
    subgraph card [Card — w-full max-w-lg]
        direction TB
        subgraph hdr [CardHeader — flex items-start justify-between gap-3]
            direction LR
            titleblock["CardTitle 遊戲帳號<br/>CardDescription '按啟動遊戲開遊戲...'"]
            logoutbtn["[登出] outline sm<br/>top-right, shrink-0"]
        end
        spawnbtn["[ 啟動遊戲 ] solid primary, full-width"]
        spawnerr["(optional) spawn error text"]
        accountlist["Account list — V3a cards"]
    end
```

Pass 3 only moves the **登出** button. It previously sat at the bottom
of `CardContent` (centred, outline) and stole vertical space below
the account list. Moved to the top-right corner of `CardHeader` as a
small outline button — small target, always within thumb reach, no
longer pushes the visible content down. The bottom `CardContent`
block now ends with the account list.

`CardHeader` becomes a flex row so the title/description block can
sit on the left and grow, while the button sits on the right and
keeps a fixed size (`shrink-0`).

