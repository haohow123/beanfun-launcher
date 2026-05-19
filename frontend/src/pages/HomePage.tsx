import { useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useSetAtom } from "jotai";
import { useEffect, useState } from "react";

import { type Account } from "@bindings/beanfun";
import { AppShell } from "@/components/layout/AppShell";
import { Hero } from "@/components/layout/Hero";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useAccountsQuery } from "@/queries/accounts";
import {
  useFetchOTPMutation,
  useSpawnAndInjectMutation,
} from "@/queries/launch";
import { loggedInAtom } from "@/state/auth";

interface FallbackOTP {
  sid: string;
  otp: string;
}

type LaunchStatus = "idle" | "pending" | "error" | "success" | "fallback";

// statusPillFor maps the per-account launch state to the top-right
// pill on the card. Returning null hides the pill (idle).
function statusPillFor(
  s: LaunchStatus,
): { label: string; className: string } | null {
  switch (s) {
    case "pending":
      return {
        label: "啟動並帶入中…",
        className: "bg-blue-500/15 text-blue-700 dark:text-blue-300",
      };
    case "success":
      return {
        label: "✓ 已帶入",
        className:
          "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
      };
    case "fallback":
      return {
        label: "手動貼上",
        className: "bg-amber-500/15 text-amber-700 dark:text-amber-300",
      };
    case "error":
      return { label: "失敗", className: "bg-destructive/15 text-destructive" };
    default:
      return null;
  }
}

// failReasonHint converts the backend's failReason code into a
// user-readable line shown above the fallback OTP. Each variant
// matches one SpawnAndInject failure path; an unknown code falls
// back to a generic message.
function failReasonHint(reason: string): string {
  switch (reason) {
    case "no-window":
      return "找不到遊戲視窗,改用下方 OTP 手動貼上。";
    case "form-not-ready":
      return "登入畫面遲遲沒就緒,改用下方 OTP 手動貼上。";
    case "inject-failed":
      return "鍵盤注入失敗,改用下方 OTP 手動貼上。";
    case "no-transition":
      return "未偵測到登入成功,可能 OTP 已輸入但要再按一次 Enter,或改用下方 OTP 手動貼上。";
    default:
      return "自動帶入失敗,改用下方 OTP 手動貼上。";
  }
}

export function HomePage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const qc = useQueryClient();
  const accounts = useAccountsQuery();
  const spawnAndInject = useSpawnAndInjectMutation();
  const fetchOTP = useFetchOTPMutation();
  const [copiedSid, setCopiedSid] = useState<string | null>(null);
  const [fallbackOTP, setFallbackOTP] = useState<FallbackOTP | null>(null);

  // Reset the per-account 啟動並帶入 state when the watcher reports
  // the game process exited (issue #62 — same hook M9's bgtask
  // watcher emits). User coming back to launcher sees a clean
  // button instead of stale "✓ 已帶入" from the last session.
  useEffect(() => {
    return Events.On("game:exited", () => {
      spawnAndInject.reset();
      setFallbackOTP(null);
    });
  }, [spawnAndInject]);

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

  function launchWithInject(acc: Account) {
    setFallbackOTP(null);
    spawnAndInject.mutate(acc, {
      onSuccess: (result) => {
        // autoFilled=false is the fallback path: backend already
        // declined to submit credentials (form not ready, etc.)
        // and surfaced the OTP for clipboard-paste. otp is empty
        // on the happy path so we never expose it when the auto
        // flow succeeded.
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

  function launchStatusFor(acc: Account): LaunchStatus {
    if (spawnAndInject.variables?.sid !== acc.sid) return "idle";
    if (spawnAndInject.isPending) return "pending";
    if (spawnAndInject.isError) return "error";
    if (spawnAndInject.isSuccess) {
      return spawnAndInject.data?.autoFilled ? "success" : "fallback";
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

  function renderAccountCard(acc: Account) {
    const launchStatus = launchStatusFor(acc);
    const copyStatus = copyStatusFor(acc);
    const anyPending =
      launchStatus === "pending" || copyStatus === "pending";
    const fallback = fallbackOTP?.sid === acc.sid ? fallbackOTP : null;
    const pill = statusPillFor(launchStatus);
    const failReason =
      launchStatus === "fallback" ? spawnAndInject.data?.failReason : undefined;

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

        {failReason && (
          <p className="mt-2 text-xs text-muted-foreground">
            {failReasonHint(failReason)}
          </p>
        )}
        {launchStatus === "error" && spawnAndInject.error && (
          <p className="mt-2 break-words text-xs text-destructive">
            {String(spawnAndInject.error)}
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
            onClick={() => launchWithInject(acc)}
          >
            啟動並帶入
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
    <AppShell mainClassName="flex-col">
      <Hero
        action={
          <Button
            variant="outline"
            size="sm"
            className="bg-background/80 backdrop-blur"
            onClick={logout}
          >
            登出
          </Button>
        }
      />

      {/* Body — accounts list only. The pre-M10 global [啟動遊戲]
          button was dropped per Q1=A in the plan (per-account card
          owns the entire launch trigger now); the M8 [帶入帳密] +
          M8 [複製帳密] pair becomes [複製帳密] + [啟動並帶入]. */}
      <section className="flex flex-1 flex-col gap-3 px-4 pb-4">
        <p className="text-xs font-medium text-muted-foreground">分帳</p>
        {renderAccounts()}
      </section>
    </AppShell>
  );
}
