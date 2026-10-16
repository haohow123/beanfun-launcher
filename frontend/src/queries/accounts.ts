import { useQuery } from "@tanstack/react-query";

import { LoginService } from "@bindings/beanfun";

export const accountsQueryPrefix = ["accounts"] as const;

export const accountsQueryKey = (loginID: string) =>
  [...accountsQueryPrefix, loginID] as const;

export function useAccountsQuery(loginID: string) {
  return useQuery({
    queryKey: accountsQueryKey(loginID),
    queryFn: () => LoginService.GetAccounts(loginID),
  });
}
