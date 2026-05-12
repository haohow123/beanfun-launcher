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

export interface UseQRPollingOptions {
  /**
   * Called once when the poll loop transitions to `approved`. Lets the
   * caller swap pages / kick off post-login work without subscribing
   * to state changes via useEffect — the React docs flag that as
   * an anti-pattern for "notify parent about state changes".
   */
  onApproved?: () => void;
}

/**
 * useQRPolling drives the QR-login state machine that pungin/Beanfun
 * observed: start → poll every 2s → flip to approved / expired / error.
 *
 * It is intentionally backend-agnostic — Day 2 backed onto a mocked Go
 * service; later milestones replace the bindings' implementation with
 * real HTTP without changing this hook.
 */
export function useQRPolling({ onApproved }: UseQRPollingOptions = {}) {
  const [state, setState] = useState<QRPollingState>({ kind: "idle" });
  // cancelled is a ref so the recursive poll closure sees the latest
  // value (a state-based flag would be stale-captured between renders).
  const cancelled = useRef(false);
  // Stable ref to the latest onApproved so start()'s closure can fire
  // the most recent callback without re-creating itself on every render.
  const onApprovedRef = useRef(onApproved);
  onApprovedRef.current = onApproved;

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
            onApprovedRef.current?.();
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
