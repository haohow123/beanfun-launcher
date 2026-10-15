import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSetAtom } from "jotai";

import { LoginService } from "@bindings/beanfun";
import { loggedInAtom } from "@/state/auth";

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
