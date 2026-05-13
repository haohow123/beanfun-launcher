import { useQueryClient } from "@tanstack/react-query";
import { useSetAtom } from "jotai";

import { type Account } from "@bindings/beanfun";
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
import { useLaunchGameMutation } from "@/queries/launch";
import { loggedInAtom } from "@/state/auth";

export function HomePage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const qc = useQueryClient();
  const accounts = useAccountsQuery();
  const launch = useLaunchGameMutation();

  function logout() {
    qc.clear();
    setLoggedIn(false);
  }

  function retry() {
    accounts.refetch();
  }

  // Per-account status derived from the (shared) launch mutation:
  // the mutation's `variables.sid` is set while pending/error/success
  // for whichever row was last clicked, so other rows stay idle.
  function statusFor(acc: Account): "idle" | "pending" | "error" | "success" {
    if (launch.variables?.sid !== acc.sid) {
      return "idle";
    }
    if (launch.isPending) return "pending";
    if (launch.isError) return "error";
    if (launch.isSuccess) return "success";
    return "idle";
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
        {accounts.data.map((acc) => {
          const status = statusFor(acc);
          return (
            <li key={acc.sid}>
              <button
                type="button"
                onClick={() => launch.mutate(acc)}
                disabled={status === "pending"}
                className="flex w-full items-center justify-between rounded-md border p-3 text-left transition-colors hover:bg-accent/40 disabled:cursor-not-allowed disabled:opacity-60"
              >
                <span className="flex flex-col">
                  <span className="text-sm font-medium">{acc.sname}</span>
                  <span className="text-xs text-muted-foreground">
                    {acc.sid}
                  </span>
                </span>
                {status === "pending" && (
                  <span className="text-xs text-muted-foreground">
                    啟動中…
                  </span>
                )}
                {status === "success" && (
                  <span className="text-xs text-foreground">✓ 已啟動</span>
                )}
                {status === "error" && (
                  <span
                    className="text-xs text-destructive"
                    title={String(launch.error)}
                  >
                    啟動失敗
                  </span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <AppShell>
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>遊戲帳號</CardTitle>
          <CardDescription>點擊帳號啟動遊戲</CardDescription>
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
