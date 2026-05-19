# eventprobe

One-off diagnostic tool. Captures a target Win32 window's UI events
via `SetWinEventHook` and streams them to stdout, so we can pick
the right event to subscribe to in **M10** (1-click 啟動+帶入,
auto-inject credentials the moment the MapleStory login form is
ready to receive `WM_CHAR`).

Not part of the production launcher. Lives under `cmd/` so any
future "did Gamania rename the window class?" debug session can
recompile + rerun.

## Build

From the repo root, on any platform:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o eventprobe.exe ./cmd/eventprobe
```

Copy `eventprobe.exe` to your Windows machine.

## Usage — two terminals

**Terminal 1 — start the probe first.** It polls every 500 ms for
a window matching one of the `--class` values until found (or the
2 min timeout fires); once found, it derives the PID and installs
a Win32 WinEvent hook for that whole process.

```cmd
eventprobe.exe --class MapleStoryClass,MapleStoryClassTW > probe.log
```

`--class` accepts a comma-separated list — the probe tries each
per tick and the first match wins. The two MapleStory class names
above mirror the two-tier fallback in
`internal/launcher/inject_windows.go`; pass both to avoid having
to guess which one TW actually uses on this Beanfun build.

You can also target by PID if you already know it:

```cmd
eventprobe.exe --pid 12345 > probe.log
```

**Terminal 2 — launch MapleStory.** Either via alpha.28's
beanfun-launcher or Beanfun's official launcher. Both produce the
same window-class events.

The probe will start streaming events as soon as the matching
window appears.

When the game's login form is visible *and accepts typed input*,
press **Ctrl-C** in Terminal 1 to stop the probe. The probe posts
a `WM_QUIT` to itself and exits cleanly.

## What to report back

Send `probe.log` (or the last ~50 lines — the ones around the
moment the login form became interactive). M10b will use this
data to pick the right `SetWinEventHook` event filter for
auto-inject.

The interesting event is usually the **first `EVENT_OBJECT_FOCUS`**
on a child control whose class is `Edit` (or similar text-input
class) — that's the moment the form is ready for `WM_CHAR`. But
this is the whole point of the spike: confirm it empirically
rather than guess.

## Expected output shape

```
eventprobe — watching hwnd=0x...  pid=...  class="MapleStoryClass"
Press Ctrl-C to stop.

T+  0.123s  EVENT_OBJECT_CREATE          hwnd=0x...  obj=0  child=0  class="MapleStoryClass"
T+  0.456s  EVENT_OBJECT_SHOW            hwnd=0x...  obj=0  child=0  class="MapleStoryClass"
T+  2.789s  EVENT_OBJECT_CREATE          hwnd=0x...  obj=0  child=0  class="Edit"
T+  2.812s  EVENT_OBJECT_FOCUS           hwnd=0x...  obj=0  child=0  class="Edit"
...
```

## Why a separate binary

Originally considered embedding this as a dev-mode toggle in the
launcher itself (env-var-gated). Rejected because:

- This is a one-shot spike, not a feature. Shouldn't pollute the
  release pipeline or the launcher's log file format.
- Standalone tool can target Beanfun's official launcher just as
  easily (`--pid <official launcher's game PID>`), useful for
  cross-checking that our spawned game emits the same events.
- Easy to revisit later (recompile + run) if Gamania changes
  window class names or event timing.

Safe to delete once M10 ships if you don't expect to re-run it.
