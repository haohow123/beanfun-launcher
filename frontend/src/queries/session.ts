import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useSetAtom } from "jotai";
import { useCallback, useEffect } from "react";

import { LoginService } from "@bindings/beanfun";
import { loggedInAtom, sessionEndReasonAtom } from "@/state/auth";

// Must match beanfun.SessionEndedEvent in internal/beanfun/service.go.
const SESSION_ENDED_EVENT = "beanfun:session-ended";

/** useLocalSignOut returns to the login page, flagged as expired, without touching the backend (which has already ended the session). */
export function useLocalSignOut() {
  const qc = useQueryClient();
  const setLoggedIn = useSetAtom(loggedInAtom);
  const setReason = useSetAtom(sessionEndReasonAtom);
  return useCallback(() => {
    qc.clear();
    setReason("expired");
    setLoggedIn(false);
  }, [qc, setLoggedIn, setReason]);
}

/** useSessionEndedBridge returns to the login page, with a reason, when keep-alive finds the session logged out. */
export function useSessionEndedBridge() {
  const signOut = useLocalSignOut();
  useEffect(() => Events.On(SESSION_ENDED_EVENT, signOut), [signOut]);
}

/** useLogoutMutation ends the backend session, then drops back to the login page even if Reset fails. */
export function useLogoutMutation() {
  const qc = useQueryClient();
  const setLoggedIn = useSetAtom(loggedInAtom);
  return useMutation({
    mutationFn: () => LoginService.Reset(),
    onError: (err) => console.error("logout: Reset failed", err),
    onSettled: () => {
      qc.clear();
      setLoggedIn(false);
    },
  });
}
