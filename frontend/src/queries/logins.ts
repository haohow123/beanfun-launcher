import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";

import { LoginInfo, LoginService } from "@bindings/beanfun";
import { accountsQueryKey } from "@/queries/accounts";
import { resetQRQueries } from "@/queries/qrLogin";

export const loginsQueryKey = ["logins"] as const;

// Must match maxLogins in internal/beanfun/service.go.
export const MAX_LOGINS = 5;

// Must match beanfun.LoginsChangedEvent in internal/beanfun/service.go.
const LOGINS_CHANGED_EVENT = "beanfun:logins-changed";

export function useLoginsQuery() {
  return useQuery({
    queryKey: loginsQueryKey,
    queryFn: () => LoginService.ListLogins(),
  });
}

/** useLoginsChangedBridge funnels the backend's login-list pushes into the loginsQueryKey cache. */
export function useLoginsChangedBridge() {
  const qc = useQueryClient();
  useEffect(() => {
    return Events.On(LOGINS_CHANGED_EVENT, (e) => {
      const raw: unknown[] = Array.isArray(e.data) ? e.data : [];
      const next: LoginInfo[] = raw.map((l) => LoginInfo.createFrom(l));
      qc.setQueryData(loginsQueryKey, next);
    });
  }, [qc]);
}

/** useLogoutLoginMutation ends one login and drops its cached accounts, even if the backend call fails. */
export function useLogoutLoginMutation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (loginID: string) => LoginService.Logout(loginID),
    onError: (err) => console.error("logout: Logout failed", err),
    onSettled: (_data, _err, loginID) => {
      qc.removeQueries({ queryKey: accountsQueryKey(loginID) });
      resetQRQueries(qc);
      qc.invalidateQueries({ queryKey: loginsQueryKey });
    },
  });
}

/** useLogoutAllMutation ends every login, then wipes the cache even if Reset fails. */
export function useLogoutAllMutation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => LoginService.Reset(),
    onError: (err) => console.error("logout: Reset failed", err),
    onSettled: () => qc.clear(),
  });
}
