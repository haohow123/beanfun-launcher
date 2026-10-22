import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useSetAtom } from "jotai";
import { useEffect } from "react";

import { LoginService } from "@bindings/beanfun";
import { accountsQueryKey, accountsQueryPrefix } from "@/queries/accounts";
import { resetQRQueries } from "@/queries/qrLogin";
import { settingsStatusQueryKey } from "@/queries/settings";
import { sessionEndNoticeAtom } from "@/state/auth";

export const loginsQueryKey = ["logins"] as const;

// Must match maxLogins in internal/beanfun/service.go.
export const MAX_LOGINS = 5;

// Must match maxLoginNameRunes in internal/beanfun/service.go.
export const MAX_LOGIN_NAME_LENGTH = 12;

// Must match beanfun.LoginsChangedEvent in internal/beanfun/service.go.
const LOGINS_CHANGED_EVENT = "beanfun:logins-changed";

export function useLoginsQuery() {
  return useQuery({
    queryKey: loginsQueryKey,
    queryFn: () => LoginService.ListLogins(),
  });
}

/** useLoginsChangedBridge treats the backend's login-list push as a signal to re-read, so the backend's answer wins over event ordering. */
export function useLoginsChangedBridge() {
  const qc = useQueryClient();
  useEffect(() => {
    return Events.On(LOGINS_CHANGED_EVENT, () => {
      qc.invalidateQueries({ queryKey: loginsQueryKey });
    });
  }, [qc]);
}

/** useLogoutLoginMutation ends one login and drops its cached accounts, even if the backend call fails. */
export function useLogoutLoginMutation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (loginID: string) => LoginService.Logout(loginID),
    onMutate: () => resetQRQueries(qc),
    onError: (err) => console.error("logout: Logout failed", err),
    onSettled: (_data, _err, loginID) => {
      qc.removeQueries({ queryKey: accountsQueryKey(loginID) });
      qc.invalidateQueries({ queryKey: loginsQueryKey });
    },
  });
}

/** useRenameLoginMutation names one login, and an empty name restores its default label. */
export function useRenameLoginMutation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ loginId, name }: { loginId: string; name: string }) =>
      LoginService.RenameLogin(loginId, name),
    onSuccess: () => qc.invalidateQueries({ queryKey: loginsQueryKey }),
    onSettled: () => qc.invalidateQueries({ queryKey: settingsStatusQueryKey }),
  });
}

/** useLogoutAllMutation ends every login and drops their cached accounts even if Reset fails. */
export function useLogoutAllMutation() {
  const qc = useQueryClient();
  const setNotice = useSetAtom(sessionEndNoticeAtom);
  return useMutation({
    mutationFn: () => LoginService.Reset(),
    onMutate: () => {
      resetQRQueries(qc);
      qc.removeQueries({ queryKey: accountsQueryPrefix });
      setNotice(null);
    },
    onError: (err) => console.error("logout: Reset failed", err),
    onSettled: () => qc.invalidateQueries({ queryKey: loginsQueryKey }),
  });
}
