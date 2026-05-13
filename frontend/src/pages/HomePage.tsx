import { useQueryClient } from "@tanstack/react-query";
import { useSetAtom } from "jotai";

import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useAccountsQuery } from "@/queries/accounts";
import { loggedInAtom } from "@/state/auth";

export function HomePage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const qc = useQueryClient();
  const accounts = useAccountsQuery();

  function logout() {
    qc.clear();
    setLoggedIn(false);
  }

  // Wrappers exist because accounts.refetch's signature is
  // (options?: RefetchOptions) => Promise<...>, which TS won't accept
  // as an onClick handler (MouseEvent isn't RefetchOptions).
  function retry() {
    accounts.refetch();
  }

  function renderAccounts() {
    if (accounts.isPending) {
      return (
        <p className="text-sm text-muted-foreground">載入帳號中…</p>
      );
    }
    if (accounts.isError) {
      return (
        <div className="flex flex-col items-center gap-2">
          <p className="text-sm text-destructive">
            載入失敗:{String(accounts.error)}
          </p>
          <Button variant="outline" onClick={retry}>
            重試
          </Button>
        </div>
      );
    }
    if (accounts.data.length === 0) {
      return (
        <div className="flex flex-col items-center gap-2">
          <p className="text-sm text-muted-foreground">找不到遊戲帳號</p>
          <Button variant="outline" onClick={retry}>
            重試
          </Button>
        </div>
      );
    }
    return (
      <ul className="flex flex-col gap-2">
        {accounts.data.map((acc) => (
          <li
            key={acc.sid}
            className="flex items-center justify-between rounded-md border p-3"
          >
            <span className="text-sm font-medium">{acc.sname}</span>
            <span className="text-xs text-muted-foreground">{acc.sid}</span>
          </li>
        ))}
      </ul>
    );
  }

  return (
    <AppShell>
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>遊戲帳號</CardTitle>
          <CardDescription>選擇要啟動的遊戲帳號</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {renderAccounts()}
          <Button
            variant="outline"
            className="mt-2 self-center"
            onClick={logout}
          >
            登出
          </Button>
        </CardContent>
      </Card>
    </AppShell>
  );
}
