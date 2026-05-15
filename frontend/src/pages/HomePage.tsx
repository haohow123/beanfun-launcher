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
import { cn } from "@/lib/utils";
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

type LaunchStatus =
  | "idle"
  | "pending"
  | "error"
  | "success"
  | "fallback"
  | "no-window";

// statusPillFor maps the per-account launch state to the top-right
// pill on the card. Returning null hides the pill (idle).
function statusPillFor(
  s: LaunchStatus,
): { label: string; className: string } | null {
  switch (s) {
    case "pending":
      return {
        label: "帶入中…",
        className: "bg-blue-500/15 text-blue-700 dark:text-blue-300",
      };
    case "success":
      return {
        label: "✓ 已送出",
        className:
          "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
      };
    case "fallback":
      return {
        label: "手動貼上",
        className: "bg-amber-500/15 text-amber-700 dark:text-amber-300",
      };
    case "no-window":
      return {
        label: "請先按啟動",
        className: "bg-muted text-muted-foreground",
      };
    case "error":
      return { label: "失敗", className: "bg-destructive/15 text-destructive" };
    default:
      return null;
  }
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

  // The backend detects an expired Beanfun session (server returns
  // 「尚未登入」) and folds it into ErrLoginRequired after Reset()ing
  // its local state. Match that on the frontend and force the user
  // back to QR login automatically — same effect as clicking 登出,
  // wrapped so we don't have to re-implement it at every mutation
  // onError site.
  function handleMutationError(err: unknown) {
    if (err instanceof Error && err.message.includes("login required")) {
      logout();
    }
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
    const blobPromise = fetchOTP
      .mutateAsync(acc)
      .then((otp) => new Blob([`${acc.sid}\n${otp}`], { type: "text/plain" }))
      .catch((e) => {
        handleMutationError(e);
        throw e;
      });
    navigator.clipboard
      .write([new ClipboardItem({ "text/plain": blobPromise })])
      .then(() => markCopied(acc.sid))
      .catch((e) => console.error("clipboard write failed:", e));
  }

  function spawnGame() {
    setFallbackOTP(null);
    spawn.mutate(undefined, { onError: handleMutationError });
  }

  function injectCredentials(acc: Account) {
    setFallbackOTP(null);
    launch.mutate(acc, {
      onSuccess: (result) => {
        if (!result.autoFilled && result.otp) {
          setFallbackOTP({ sid: acc.sid, otp: result.otp });
        }
      },
      onError: handleMutationError,
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
    const pill = statusPillFor(launchStatus);

    return (
      <li
        key={acc.sid}
        className="rounded-lg border p-3 transition-colors hover:bg-muted/40"
      >
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-semibold leading-tight">
              {acc.sname}
            </div>
            <code className="mt-0.5 block break-all text-xs text-muted-foreground">
              {acc.sid}
            </code>
          </div>
          {pill && (
            <span
              className={cn(
                "shrink-0 whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium",
                pill.className,
              )}
            >
              {pill.label}
            </span>
          )}
        </div>

        {launchStatus === "success" && (
          <p className="mt-2 text-xs text-muted-foreground">
            若沒帶入,點一下遊戲帳號欄位讓游標進入,再按一次
          </p>
        )}
        {launchStatus === "error" && launch.error && (
          <p className="mt-2 break-words text-xs text-destructive">
            {String(launch.error)}
          </p>
        )}

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
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0 flex-1">
              <CardTitle>遊戲帳號</CardTitle>
              <CardDescription>
                按「啟動遊戲」開遊戲,登入畫面出現後再按帳號旁邊的「帶入帳密」
              </CardDescription>
            </div>
            <Button
              variant="outline"
              size="sm"
              className="shrink-0"
              onClick={logout}
            >
              登出
            </Button>
          </div>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Button onClick={spawnGame} disabled={spawn.isPending}>
            {spawnLabel()}
          </Button>
          {spawn.isError && (
            <p className="break-words text-xs text-destructive">
              啟動失敗:{String(spawn.error)}
            </p>
          )}

          {renderAccounts()}
        </CardContent>
      </Card>
    </AppShell>
  );
}
