import { useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useSetAtom, useStore } from "jotai";
import { useEffect } from "react";

import { accountsQueryKey } from "@/queries/accounts";
import { loginsQueryKey } from "@/queries/logins";
import { resetQRQueries } from "@/queries/qrLogin";
import { addingLoginAtom, sessionEndNoticeAtom } from "@/state/auth";

// Must match beanfun.SessionEndedEvent in internal/beanfun/service.go.
const SESSION_ENDED_EVENT = "beanfun:session-ended";

/** useSessionEndedBridge drops only the login keep-alive found logged out, and tells the user which one. */
export function useSessionEndedBridge() {
  const qc = useQueryClient();
  const store = useStore();
  const setNotice = useSetAtom(sessionEndNoticeAtom);
  useEffect(() => {
    return Events.On(SESSION_ENDED_EVENT, (e) => {
      const { loginId, label } = e.data as { loginId: string; label: string };
      qc.removeQueries({ queryKey: accountsQueryKey(loginId) });
      if (!store.get(addingLoginAtom)) resetQRQueries(qc);
      setNotice({ label });
      qc.invalidateQueries({ queryKey: loginsQueryKey });
    });
  }, [qc, store, setNotice]);
}
