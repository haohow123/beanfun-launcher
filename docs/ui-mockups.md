# UI mockups

Source of truth for the front-end layout before code lands. Add a new
section whenever a non-trivial UI change is in flight — mermaid
diagram first, then implement.

The diagrams are intentionally rough. They pin layout / hierarchy
decisions so a reviewer doesn't need to run the app to tell what's
proposed. Tailwind class names and exact pixel sizes live in the
code; this file owns the *shape*.

---

## HomePage — account card (Pass 2)

Shipped in PR #49.

```mermaid
flowchart TB
    subgraph card [Account card — rounded-lg border, hover bg-muted/40]
        direction TB
        subgraph header [Header row — flex items-start gap-3]
            direction LR
            avatar["Avatar<br/>40×40 rounded-lg<br/>1st char of sname<br/>deterministic colour"]
            nameblock["sname (semibold)<br/>sid (muted xs code)"]
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

### Avatar palette

Six Tailwind colours bucketed by `djb2(sname) mod 6` so the same account
keeps the same colour across renders / restarts / re-logins:
`bg-orange-500`, `bg-blue-500`, `bg-emerald-500`, `bg-purple-500`,
`bg-pink-500`, `bg-cyan-500`.
