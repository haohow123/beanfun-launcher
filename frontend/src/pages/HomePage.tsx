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
import { useGameAccounts } from "@/hooks/useGameAccounts";
import { loggedInAtom } from "@/state/auth";

export function HomePage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const { state, refetch } = useGameAccounts();

  return (
    <AppShell>
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>遊戲帳號</CardTitle>
          <CardDescription>選擇要啟動的遊戲帳號</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {state.kind === "loading" && (
            <p className="text-sm text-muted-foreground">載入帳號中…</p>
          )}

          {state.kind === "ready" && state.accounts.length === 0 && (
            <div className="flex flex-col items-center gap-2">
              <p className="text-sm text-muted-foreground">找不到遊戲帳號</p>
              <Button variant="outline" onClick={refetch}>
                重試
              </Button>
            </div>
          )}

          {state.kind === "ready" && state.accounts.length > 0 && (
            <ul className="flex flex-col gap-2">
              {state.accounts.map((acc) => (
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

          {state.kind === "error" && (
            <div className="flex flex-col items-center gap-2">
              <p className="text-sm text-destructive">
                載入失敗:{state.message}
              </p>
              <Button variant="outline" onClick={refetch}>
                重試
              </Button>
            </div>
          )}

          <Button
            variant="outline"
            className="mt-2 self-center"
            onClick={() => setLoggedIn(false)}
          >
            登出
          </Button>
        </CardContent>
      </Card>
    </AppShell>
  );
}
