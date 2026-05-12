import { useCallback, useEffect, useRef, useState } from "react";

import { LoginService, QRStatus, type QRStart } from "@bindings/beanfun";

const POLL_INTERVAL_MS = 2000;

export type QRPollingState =
  | { kind: "idle" }
  | { kind: "starting" }
  | { kind: "polling"; qr: QRStart }
  | { kind: "expired" }
  | { kind: "approved" }
  | { kind: "error"; message: string };

/**
 * useQRPolling drives the QR-login state machine pungin/Beanfun observed:
 * start → poll every 2s → flip to approved / expired / error.
 *
 * It is intentionally backend-agnostic — Day 2 backs onto a mocked Go
 * service; Day 3+ replaces the bindings' implementation with real HTTP
 * to login.beanfun.com without changing this hook.
 */
export function useQRPolling() {
  const [state, setState] = useState<QRPollingState>({ kind: "idle" });
  // cancelled is a ref so the recursive poll closure sees the latest value
  // (a state-based flag would be stale-captured between renders).
  const cancelled = useRef(false);

  const start = useCallback(async () => {
    cancelled.current = false;
    setState({ kind: "starting" });

    let qr: QRStart;
    try {
      qr = await LoginService.StartQRLogin();
    } catch (err) {
      setState({ kind: "error", message: String(err) });
      return;
    }
    if (cancelled.current) return;
    setState({ kind: "polling", qr });

    const tick = async () => {
      if (cancelled.current) return;
      try {
        const status = await LoginService.CheckQRLogin();
        if (cancelled.current) return;
        switch (status) {
          case QRStatus.QRStatusApproved:
            setState({ kind: "approved" });
            return;
          case QRStatus.QRStatusExpired:
            setState({ kind: "expired" });
            return;
          // Pending and Retry both mean "keep polling".
          default:
            window.setTimeout(tick, POLL_INTERVAL_MS);
        }
      } catch (err) {
        if (cancelled.current) return;
        setState({ kind: "error", message: String(err) });
      }
    };
    window.setTimeout(tick, POLL_INTERVAL_MS);
  }, []);

  useEffect(() => {
    return () => {
      cancelled.current = true;
    };
  }, []);

  return { state, start };
}
