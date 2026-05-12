import { useSetAtom } from "jotai";
import { useCallback, useEffect, useRef, useState } from "react";

import { LoginService, QRStatus, type QRStart } from "@bindings/beanfun";
import { loggedInAtom } from "@/state/auth";

const POLL_INTERVAL_MS = 2000;

export type QRPollingState =
  | { kind: "idle" }
  | { kind: "starting" }
  | { kind: "polling"; qr: QRStart }
  | { kind: "expired" }
  | { kind: "approved" }
  | { kind: "error"; message: string };

/**
 * useQRPolling drives the QR-login state machine: start → poll every 2s
 * → flip to approved / expired / error. See docs/beanfun-login-protocol.md
 * for the wire spec.
 *
 * The hook owns the "approved → loggedInAtom = true" transition because
 * that is the only valid completion of a QR-login flow — pages that use
 * it never need to handle approval themselves.
 */
export function useQRPolling() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const [state, setState] = useState<QRPollingState>({ kind: "idle" });
  // cancelled is a ref so the recursive poll closure sees the latest
  // value (a state-based flag would be stale-captured between renders).
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
            setLoggedIn(true);
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
  }, [setLoggedIn]);

  useEffect(() => {
    return () => {
      cancelled.current = true;
    };
  }, []);

  return { state, start };
}
