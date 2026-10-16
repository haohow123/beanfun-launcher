import { useQuery } from "@tanstack/react-query";

import { LoginService } from "@bindings/beanfun";

export const accountsQueryKey = (loginID: string) =>
  ["accounts", loginID] as const;

export function useAccountsQuery(loginID: string) {
  return useQuery({
    queryKey: accountsQueryKey(loginID),
    queryFn: () => LoginService.GetAccounts(loginID),
  });
}
