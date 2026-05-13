import { useQuery } from "@tanstack/react-query";

import { LoginService } from "@bindings/beanfun";

export const accountsQueryKey = ["accounts"] as const;

export function useAccountsQuery() {
  return useQuery({
    queryKey: accountsQueryKey,
    queryFn: () => LoginService.GetAccounts(),
  });
}
