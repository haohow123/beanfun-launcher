import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSetAtom } from "jotai";

import { LoginService } from "@bindings/beanfun";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { loggedInAtom } from "@/state/auth";

export function HomePage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const qc = useQueryClient();
  const { data, isPending, isError, error, refetch } = useQuery({
    queryKey: ["accounts"],
    queryFn: () => LoginService.GetAccounts(),
  });

  // Wipe all cached query state on logout — otherwise LoginPage
  // remounts and reads the stale ['qrStatus'] = 'approved' from cache,
  // its useEffect fires, and we ping-pong straight back here.
  const logout = () => {
    qc.clear();
    setLoggedIn(false);
  };

  return (
    <AppShell>
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>遊戲帳號</CardTitle>
          <CardDescription>選擇要啟動的遊戲帳號</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {isPending && (
            <p className="text-sm text-muted-foreground">載入帳號中…</p>
          )}

          {isError && (
            <div className="flex flex-col items-center gap-2">
              <p className="text-sm text-destructive">
                載入失敗:{String(error)}
              </p>
              <Button variant="outline" onClick={() => refetch()}>
                重試
              </Button>
            </div>
          )}

          {!isPending && !isError && data && data.length === 0 && (
            <div className="flex flex-col items-center gap-2">
              <p className="text-sm text-muted-foreground">找不到遊戲帳號</p>
              <Button variant="outline" onClick={() => refetch()}>
                重試
              </Button>
            </div>
          )}

          {!isPending && !isError && data && data.length > 0 && (
            <ul className="flex flex-col gap-2">
              {data.map((acc) => (
                <li
                  key={acc.sid}
                  data-disabled={!acc.enabled || undefined}
                  className="flex items-center justify-between rounded-md border p-3 data-[disabled]:opacity-50"
                >
                  <span className="text-sm font-medium">{acc.sname}</span>
                  <span className="text-xs text-muted-foreground">
                    {acc.sid}
                  </span>
                </li>
              ))}
            </ul>
          )}

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
