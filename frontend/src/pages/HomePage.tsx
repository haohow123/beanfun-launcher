import { useQueryClient } from "@tanstack/react-query";
import { useSetAtom } from "jotai";
import { useState } from "react";

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
import {
  useFetchOTPMutation,
  useLaunchGameMutation,
} from "@/queries/launch";
import { loggedInAtom } from "@/state/auth";

export function HomePage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const qc = useQueryClient();
  const accounts = useAccountsQuery();
  const launch = useLaunchGameMutation();
  const fetchOTP = useFetchOTPMutation();
  const [copiedSid, setCopiedSid] = useState<string | null>(null);

  function logout() {
    qc.clear();
    setLoggedIn(false);
  }

  function retry() {
    accounts.refetch();
  }

  // Per-account launch status derived from the (shared) launch
  // mutation's variables.sid match. Other rows stay idle.
  function launchStatusFor(acc: Account): "idle" | "pending" | "error" | "success" {
    if (launch.variables?.sid !== acc.sid) return "idle";
    if (launch.isPending) return "pending";
    if (launch.isError) return "error";
    if (launch.isSuccess) return "success";
    return "idle";
  }

  // OTP status uses the same shared-mutation pattern. The OTP value
  // itself sits in fetchOTP.data when isSuccess + variables.sid match.
  function otpStatusFor(acc: Account): "idle" | "pending" | "error" | "ready" {
    if (fetchOTP.variables?.sid !== acc.sid) return "idle";
    if (fetchOTP.isPending) return "pending";
    if (fetchOTP.isError) return "error";
    if (fetchOTP.isSuccess) return "ready";
    return "idle";
  }

  async function copyToClipboard(sid: string, value: string) {
    try {
      await navigator.clipboard.writeText(value);
      setCopiedSid(sid);
      window.setTimeout(() => setCopiedSid((s) => (s === sid ? null : s)), 1500);
    } catch {
      // Clipboard API can fail in restricted contexts (no HTTPS,
      // user gesture timing). Surface a console error and let the
      // user select+copy from the readonly input manually.
      console.error("clipboard write failed");
    }
  }

  function renderAccounts() {
    if (accounts.isPending) {
      return <p className="text-sm text-muted-foreground">載入帳號中…</p>;
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
          const launchStatus = launchStatusFor(acc);
          const otpStatus = otpStatusFor(acc);
          const otpValue =
            otpStatus === "ready" ? (fetchOTP.data ?? "") : "";
          return (
            <li key={acc.sid} className="flex flex-col gap-2">
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => launch.mutate(acc)}
                  disabled={launchStatus === "pending"}
                  className="flex flex-1 items-center justify-between rounded-md border p-3 text-left transition-colors hover:bg-accent/40 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  <span className="flex flex-col">
                    <span className="text-sm font-medium">{acc.sname}</span>
                    <span className="text-xs text-muted-foreground">
                      {acc.sid}
                    </span>
                  </span>
                  {launchStatus === "pending" && (
                    <span className="text-xs text-muted-foreground">
                      啟動中…
                    </span>
                  )}
                  {launchStatus === "success" && (
                    <span className="text-xs text-foreground">✓ 已啟動</span>
                  )}
                  {launchStatus === "error" && (
                    <span
                      className="text-xs text-destructive"
                      title={String(launch.error)}
                    >
                      啟動失敗
                    </span>
                  )}
                </button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={otpStatus === "pending"}
                  onClick={() => fetchOTP.mutate(acc)}
                  title="取得 OTP 後複製貼到其他啟動器"
                >
                  {otpStatus === "pending" ? "…" : "OTP"}
                </Button>
              </div>

              {otpStatus === "ready" && (
                <div className="flex items-center gap-2 rounded-md border bg-muted/30 px-3 py-2">
                  <span className="text-xs text-muted-foreground">OTP</span>
                  <code className="flex-1 select-all font-mono text-sm">
                    {otpValue}
                  </code>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => copyToClipboard(acc.sid, otpValue)}
                  >
                    {copiedSid === acc.sid ? "✓ 已複製" : "複製"}
                  </Button>
                </div>
              )}

              {otpStatus === "error" && (
                <p
                  className="text-xs text-destructive"
                  title={String(fetchOTP.error)}
                >
                  取得 OTP 失敗:{String(fetchOTP.error)}
                </p>
              )}
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
          <CardDescription>
            點帳號啟動遊戲,或按 OTP 取得密碼複製貼到其他啟動器
          </CardDescription>
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
