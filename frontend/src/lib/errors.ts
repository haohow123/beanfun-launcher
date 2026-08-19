// Each needle is a stable substring of a Go error message. The Go
// counterpart is named on every entry because nothing enforces this
// contract — changing the Go message silently breaks the Chinese one.
const FRIENDLY: ReadonlyArray<readonly [string, string]> = [
  // internal/beanfun/errors.go ErrIPBlocked
  ["ip temporarily blocked", "beanfun 暫時鎖定此 IP，請稍後再試"],
  // internal/launcher/service.go errGameAlreadyRunning
  ["already running", "遊戲已開啟，請稍候再試"],
];

/**
 * friendlyError maps a backend error onto Chinese copy when we recognise
 * it, and falls back to the raw message otherwise. Used by every place
 * that renders a backend failure, so the same condition reads the same
 * way wherever it surfaces.
 */
export function friendlyError(err: unknown): string {
  const message = err instanceof Error ? err.message : String(err);
  for (const [needle, friendly] of FRIENDLY) {
    if (message.includes(needle)) return friendly;
  }
  return message;
}
