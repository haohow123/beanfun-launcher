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
  useSpawnGameMutation,
} from "@/queries/launch";
import { loggedInAtom } from "@/state/auth";

interface FallbackOTP {
  sid: string;
  otp: string;
}

export function HomePage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const qc = useQueryClient();
  const accounts = useAccountsQuery();
  const spawn = useSpawnGameMutation();
  const launch = useLaunchGameMutation();
  const fetchOTP = useFetchOTPMutation();
  const [copiedSid, setCopiedSid] = useState<string | null>(null);
  const [fallbackOTP, setFallbackOTP] = useState<FallbackOTP | null>(null);

  function logout() {
    qc.clear();
    setLoggedIn(false);
  }

  function retry() {
    accounts.refetch();
  }

  function markCopied(sid: string) {
    setCopiedSid(sid);
    window.setTimeout(
      () => setCopiedSid((s) => (s === sid ? null : s)),
      1500,
    );
  }

  // Why ClipboardItem + Promise<Blob> instead of plain
  // `await fetchOTP.mutateAsync(...)` then `writeText(...)`:
  // navigator.clipboard.writeText requires the document's "transient
  // user activation" — set by a click event handler — to be still
  // valid when the call is made. WebView2 / WKWebView treat the
  // activation as consumed once you `await`, so a writeText after a
  // 1-2s OTP fetch gets silently rejected. clipboard.write with a
  // ClipboardItem whose blob is a pending Promise tells the browser
  // "hold activation; resolve later" — which keeps the activation
  // budget for the actual write.
  function copyCredentials(acc: Account) {
    const blobPromise = fetchOTP.mutateAsync(acc).then(
      (otp) => new Blob([`${acc.sid}\n${otp}`], { type: "text/plain" }),
    );
    navigator.clipboard
      .write([new ClipboardItem({ "text/plain": blobPromise })])
      .then(() => markCopied(acc.sid))
      .catch((e) => console.error("clipboard write failed:", e));
  }

  function spawnGame() {
    setFallbackOTP(null);
    spawn.mutate();
  }

  function injectCredentials(acc: Account) {
    setFallbackOTP(null);
    launch.mutate(acc, {
      onSuccess: (result) => {
        if (!result.autoFilled && result.otp) {
          setFallbackOTP({ sid: acc.sid, otp: result.otp });
        }
      },
    });
  }

  function copyFallback(item: FallbackOTP) {
    navigator.clipboard
      .writeText(`${item.sid}\n${item.otp}`)
      .then(() => markCopied(item.sid))
      .catch((e) => console.error("clipboard write failed:", e));
  }

  function launchStatusFor(
    acc: Account,
  ): "idle" | "pending" | "error" | "success" | "fallback" | "no-window" {
    if (launch.variables?.sid !== acc.sid) return "idle";
    if (launch.isPending) return "pending";
    if (launch.isError) return "error";
    if (launch.isSuccess) {
      if (launch.data?.autoFilled) return "success";
      if (launch.data?.noWindow) return "no-window";
      return "fallback";
    }
    return "idle";
  }

  function copyStatusFor(acc: Account): "idle" | "pending" | "error" | "copied" {
    if (copiedSid === acc.sid) return "copied";
    if (fetchOTP.variables?.sid !== acc.sid) return "idle";
    if (fetchOTP.isPending) return "pending";
    if (fetchOTP.isError) return "error";
    return "idle";
  }

  function spawnLabel() {
    if (spawn.isPending) return "啟動中…";
    if (spawn.isSuccess) return "✓ 已啟動,等登入畫面";
    return "啟動遊戲";
  }

  function renderAccountCard(acc: Account) {
    const launchStatus = launchStatusFor(acc);
    const copyStatus = copyStatusFor(acc);
    const anyPending =
      launchStatus === "pending" || copyStatus === "pending";
    const fallback = fallbackOTP?.sid === acc.sid ? fallbackOTP : null;

    return (
      <li key={acc.sid} className="rounded-md border p-3">
        <div className="mb-1 flex items-baseline justify-between gap-2">
          <span className="text-sm font-medium">{acc.sname}</span>
          {launchStatus === "pending" && (
            <span className="text-xs text-muted-foreground">帶入中…</span>
          )}
          {launchStatus === "success" && (
            <span className="text-xs text-foreground">✓ 已帶入帳密</span>
          )}
          {launchStatus === "no-window" && (
            <span className="text-xs text-muted-foreground">
              請先按啟動遊戲
            </span>
          )}
          {launchStatus === "fallback" && (
            <span className="text-xs text-foreground">
              需手動貼上
            </span>
          )}
          {launchStatus === "error" && (
            <span
              className="text-xs text-destructive"
              title={String(launch.error)}
            >
              帶入失敗
            </span>
          )}
        </div>

        <code className="block break-all text-xs text-muted-foreground">
          {acc.sid}
        </code>

        {fallback && (
          <div className="mt-2 flex items-center gap-2 rounded-md bg-muted/50 px-2 py-1">
            <code className="flex-1 select-all break-all font-mono text-sm">
              {fallback.otp}
            </code>
            <Button
              variant="outline"
              size="sm"
              onClick={() => copyFallback(fallback)}
            >
              {copiedSid === fallback.sid ? "✓" : "複製"}
            </Button>
          </div>
        )}

        <div className="mt-3 flex justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={anyPending}
            onClick={() => copyCredentials(acc)}
            title="抓 OTP 後把帳號+密碼 (換行隔開) 複製到剪貼簿"
          >
            {copyStatus === "pending"
              ? "產生中…"
              : copyStatus === "copied"
                ? "✓ 已複製"
                : copyStatus === "error"
                  ? "複製失敗"
                  : "複製帳密"}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={anyPending}
            onClick={() => injectCredentials(acc)}
          >
            帶入帳密
          </Button>
        </div>
      </li>
    );
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
      <ul className="flex flex-col gap-3">
        {accounts.data.map(renderAccountCard)}
      </ul>
    );
  }

  return (
    <AppShell>
      <Card className="w-full max-w-lg">
        <CardHeader>
          <CardTitle>遊戲帳號</CardTitle>
          <CardDescription>
            按「啟動遊戲」開遊戲,等登入畫面出現後選帳號按「帶入帳密」
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Button
            onClick={spawnGame}
            disabled={spawn.isPending}
          >
            {spawnLabel()}
          </Button>
          {spawn.isError && (
            <p className="text-xs text-destructive" title={String(spawn.error)}>
              啟動失敗:{String(spawn.error)}
            </p>
          )}

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
