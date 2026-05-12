import { useCallback, useEffect, useRef, useState } from "react";

import { LoginService, type Account } from "@bindings/beanfun";

export type GameAccountsState =
  | { kind: "loading" }
  | { kind: "ready"; accounts: Account[] }
  | { kind: "error"; message: string };

/**
 * useGameAccounts calls LoginService.GetAccounts once on mount and
 * exposes a discriminated state + a refetch callback.
 *
 * Short-lived scaffolding — Milestone 5.5 swaps this (and useQRPolling)
 * for TanStack Query.
 */
export function useGameAccounts() {
  const [state, setState] = useState<GameAccountsState>({ kind: "loading" });
  const cancelled = useRef(false);

  const fetch = useCallback(async () => {
    cancelled.current = false;
    setState({ kind: "loading" });
    try {
      const accounts = await LoginService.GetAccounts();
      if (cancelled.current) return;
      setState({ kind: "ready", accounts });
    } catch (err) {
      if (cancelled.current) return;
      setState({ kind: "error", message: String(err) });
    }
  }, []);

  // Fire once on mount. fired-ref guards against React 19 strict-mode
  // double-invoke; cancelled is reset on every (re-)mount so the
  // in-flight result from the first invoke can still land after the
  // strict-mode unmount/remount cycle.
  const fired = useRef(false);
  useEffect(() => {
    cancelled.current = false;
    if (!fired.current) {
      fired.current = true;
      void fetch();
    }
    return () => {
      cancelled.current = true;
    };
  }, [fetch]);

  return { state, refetch: fetch };
}
