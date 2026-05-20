import { useQueryClient } from "@tanstack/react-query";
import { useSetAtom } from "jotai";
import { useState } from "react";

import { type Account } from "@bindings/beanfun";
import { AppShell } from "@/components/layout/AppShell";
import { Hero } from "@/components/layout/Hero";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useAccountsQuery } from "@/queries/accounts";
import { useGameStateQuery } from "@/queries/gameState";
import {
  useFetchOTPMutation,
  useLaunchAccountMutation,
  useSpawnGameCleanMutation,
} from "@/queries/launch";
import { loggedInAtom } from "@/state/auth";

interface FallbackOTP {
  sid: string;
  otp: string;
}

type PrimaryStatus =
  | "idle"
  | "launching"
  | "running"
  | "fallback"
  | "error";

function statusPillFor(
  s: PrimaryStatus,
): { label: string; className: string } | null {
  switch (s) {
    case "launching":
      return {
        label: "進入中…",
        className: "bg-blue-500/15 text-blue-700 dark:text-blue-300",
      };
    case "running":
      return {
        label: "遊戲執行中",
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

export function HomePage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const qc = useQueryClient();
  const accounts = useAccountsQuery();
  const gameState = useGameStateQuery();
  const launchAccount = useLaunchAccountMutation();
  const spawnGameClean = useSpawnGameCleanMutation();
  const fetchOTP = useFetchOTPMutation();
  const [copiedSid, setCopiedSid] = useState<string | null>(null);
  const [fallbackOTP, setFallbackOTP] = useState<FallbackOTP | null>(null);

  const isGameRunning = gameState.data?.running ?? false;
  const lastUsedSid = gameState.data?.lastUsedSid ?? "";

  function logout() {
    qc.clear();
    setLoggedIn(false);
  }

  function retry() {
    accounts.refetch();
  }

  // The backend folds session expiry into ErrLoginRequired (server
  // returns 「尚未登入」, backend Reset()s its local state). Match
  // that on the frontend by triggering the same path as 登出.
  function handleMutationError(err: unknown) {
    if (err instanceof Error && err.message.includes("login required")) {
      logout();
    }
  }

  // LaunchAccount swallows the spawn↔inject race internally via its
  // one-level fallback (service.go:LaunchAccount), so errGameAlreadyRunning
  // should never reach here. The substring check below is a safety net
  // in case the backend ever surfaces it — render the Chinese hint
  // rather than the raw English error.
  function friendlyMutationError(err: unknown): string | undefined {
    if (!(err instanceof Error)) return undefined;
    if (err.message.includes("already running")) {
      return "遊戲已開啟，請稍候再試";
    }
    return err.message;
  }

  function markCopied(sid: string) {
    setCopiedSid(sid);
    window.setTimeout(
      () => setCopiedSid((s) => (s === sid ? null : s)),
      1500,
    );
  }

  // ClipboardItem + Promise<Blob> preserves transient user activation
  // across the async OTP fetch — WebView2 invalidates activation on
  // await, so plain `writeText` after fetch gets silently rejected.
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

  // playAccount is the single per-account action — intent only.
  // Backend's LaunchAccount picks spawn-vs-inject based on the
  // current game-window state and handles the small race window
  // internally; the FE never decides which mutation to fire.
  function playAccount(acc: Account) {
    setFallbackOTP(null);
    launchAccount.mutate(acc, {
      onSuccess: (result) => {
        if (result.autoFilled === false && result.otp) {
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

  function primaryStatusFor(acc: Account): PrimaryStatus {
    if (launchAccount.variables?.sid === acc.sid) {
      if (launchAccount.isPending) return "launching";
      if (launchAccount.isError) return "error";
      if (
        launchAccount.isSuccess &&
        launchAccount.data?.autoFilled === false &&
        launchAccount.data?.otp
      ) {
        return "fallback";
      }
    }
    // Pill shows "遊戲執行中" only on the card whose credentials are
    // currently live in the game session — backend tracks this as
    // lastUsedSid (updated by SpawnGame + Launch, cleared by watcher
    // exit). Clean-spawned sessions have lastUsedSid="" until the
    // user injects via 進入遊戲, so no card shows the pill in that
    // window. The active mutation state above wins when this account
    // is actively launching.
    if (isGameRunning && lastUsedSid === acc.sid) return "running";
    return "idle";
  }

  function copyStatusFor(
    acc: Account,
  ): "idle" | "pending" | "error" | "copied" {
    if (copiedSid === acc.sid) return "copied";
    if (fetchOTP.variables?.sid !== acc.sid) return "idle";
    if (fetchOTP.isPending) return "pending";
    if (fetchOTP.isError) return "error";
    return "idle";
  }

  function renderAccountCard(acc: Account) {
    const primary = primaryStatusFor(acc);
    const copyStatus = copyStatusFor(acc);
    const fallback = fallbackOTP?.sid === acc.sid ? fallbackOTP : null;
    const pill = statusPillFor(primary);
    const isThisAccountPending =
      primary === "launching" || copyStatus === "pending";

    // Single intent button — backend resolves spawn-vs-inject from
    // its just-in-time view of the game window. No isGameRunning gate
    // here on purpose: even with stale FE state the action is correct,
    // because LaunchAccount + corrective emit handle the race.
    const playDisabled = isThisAccountPending || gameState.isPending;

    const errorText =
      primary === "error" ? friendlyMutationError(launchAccount.error) : undefined;

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

        {errorText && (
          <p className="mt-2 break-words text-xs text-destructive">
            {errorText}
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
            disabled={isThisAccountPending}
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
            disabled={playDisabled}
            onClick={() => playAccount(acc)}
          >
            進入遊戲
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

  function cleanSpawn() {
    setFallbackOTP(null);
    spawnGameClean.mutate(undefined, {
      onError: handleMutationError,
    });
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

      {/* Clean-spawn lives inside the main section as a direct flex
          child so the default Tailwind `align-items: stretch` gives
          it full width without an explicit w-full class — restores
          the banner-led app-shell design from a73803e (HomePage Pass 4).
          No isGameRunning gate: backend's errGameAlreadyRunning +
          corrective emit handle the case where a game is already up,
          so the button stays operable without leaving the user with
          stale-UI dead time after a game closes. */}
      <section className="flex flex-1 flex-col gap-3 px-4 pb-4 pt-2">
        <Button
          disabled={spawnGameClean.isPending || gameState.isPending}
          onClick={cleanSpawn}
        >
          {spawnGameClean.isPending ? "啟動中…" : "啟動(可切換帳號)"}
        </Button>

        <p className="mt-1 text-xs font-medium text-muted-foreground">分帳</p>
        {renderAccounts()}
      </section>
    </AppShell>
  );
}
