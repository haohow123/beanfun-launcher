import { useQueryClient } from "@tanstack/react-query";
import { useAtom, useSetAtom } from "jotai";
import { useEffect, useRef, useState } from "react";

import { type Account } from "@bindings/beanfun";
import { AppShell } from "@/components/layout/AppShell";
import { Hero } from "@/components/layout/Hero";
import { LoginGroup } from "@/components/LoginGroup";
import { Button } from "@/components/ui/button";
import { friendlyError, isGamePathError } from "@/lib/errors";
import { cn } from "@/lib/utils";
import { useGameStateQuery } from "@/queries/gameState";
import {
  useFetchOTPMutation,
  useLaunchAccountMutation,
  useSpawnGameCleanMutation,
} from "@/queries/launch";
import {
  MAX_LOGINS,
  loginsQueryKey,
  useLoginsQuery,
  useLogoutAllMutation,
  useLogoutLoginMutation,
} from "@/queries/logins";
import { resetQRQueries } from "@/queries/qrLogin";
import { addingLoginAtom, sessionEndNoticeAtom } from "@/state/auth";
import { settingsOpenAtom } from "@/state/settings";

interface FallbackOTP {
  loginId: string;
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

function cardKey(loginId: string, sid: string): string {
  return `${loginId}:${sid}`;
}

function isVariablesFor(
  v: { loginId: string; account: Account } | undefined,
  loginId: string,
  acc: Account,
): boolean {
  return v?.loginId === loginId && v.account.sid === acc.sid;
}

export function HomePage() {
  const [settingsOpen, setSettingsOpen] = useAtom(settingsOpenAtom);
  const qc = useQueryClient();
  const logoutAll = useLogoutAllMutation();
  const logoutLogin = useLogoutLoginMutation();
  const logins = useLoginsQuery();
  const setAdding = useSetAtom(addingLoginAtom);
  const [notice, setNotice] = useAtom(sessionEndNoticeAtom);
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const gameState = useGameStateQuery();
  const launchAccount = useLaunchAccountMutation();
  const spawnGameClean = useSpawnGameCleanMutation();
  const fetchOTP = useFetchOTPMutation();
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const [fallbackOTP, setFallbackOTP] = useState<FallbackOTP | null>(null);

  // Clears an errored mutation on settings close, but skips a still-pending one so its mutate()-scoped callbacks stay attached.
  const wasSettingsOpenRef = useRef(settingsOpen);
  useEffect(() => {
    if (wasSettingsOpenRef.current && !settingsOpen) {
      if (launchAccount.isError) launchAccount.reset();
      if (spawnGameClean.isError) spawnGameClean.reset();
    }
    wasSettingsOpenRef.current = settingsOpen;
  }, [settingsOpen, launchAccount, spawnGameClean]);

  const isGameRunning = gameState.data?.running ?? false;
  const lastUsedSid = gameState.data?.lastUsedSid ?? "";

  const loginList = logins.data ?? [];

  function logout() {
    logoutAll.mutate();
  }

  function logoutOne(loginId: string) {
    logoutLogin.mutate(loginId);
  }

  function addLogin() {
    resetQRQueries(qc);
    setAdding(true);
  }

  function labelFor(loginId: string | undefined): string {
    return loginList.find((l) => l.id === loginId)?.label ?? "此帳號";
  }

  // The backend has already ended that one login when it returns ErrLoginRequired, so only refresh the list and tell the user which one.
  function handleMutationError(err: unknown, loginId?: string) {
    if (!mountedRef.current) return;
    if (err instanceof Error && err.message.includes("login required")) {
      setNotice({ label: labelFor(loginId) });
      qc.invalidateQueries({ queryKey: loginsQueryKey });
    }
  }

  // LaunchAccount swallows the spawn↔inject race internally via its
  // one-level fallback (service.go:LaunchAccount), so errGameAlreadyRunning
  // should never reach here. The substring check below is a safety net
  // in case the backend ever surfaces it — render the Chinese hint
  // rather than the raw English error.
  function friendlyMutationError(err: unknown): string | undefined {
    if (!(err instanceof Error)) return undefined;
    return friendlyError(err);
  }

  function goToSettings() {
    setSettingsOpen(true);
  }

  // Shared by both launch entry points: a missing game path can only be fixed on the settings page.
  function renderGoToSettings(err: unknown) {
    if (!isGamePathError(err)) return null;
    return (
      <Button variant="outline" size="sm" className="mt-2" onClick={goToSettings}>
        前往設定
      </Button>
    );
  }

  function markCopied(key: string) {
    setCopiedKey(key);
    window.setTimeout(
      () => setCopiedKey((k) => (k === key ? null : k)),
      1500,
    );
  }

  // ClipboardItem + Promise<Blob> preserves transient user activation
  // across the async OTP fetch — WebView2 invalidates activation on
  // await, so plain `writeText` after fetch gets silently rejected.
  function copyCredentials(loginId: string, acc: Account) {
    const blobPromise = fetchOTP
      .mutateAsync({ loginId, account: acc })
      .then((otp) => new Blob([`${acc.sid}\n${otp}`], { type: "text/plain" }))
      .catch((e) => {
        handleMutationError(e, loginId);
        throw e;
      });
    navigator.clipboard
      .write([new ClipboardItem({ "text/plain": blobPromise })])
      .then(() => markCopied(cardKey(loginId, acc.sid)))
      .catch((e) => console.error("clipboard write failed:", e));
  }

  // playAccount is the single per-account action — intent only.
  // Backend's LaunchAccount picks spawn-vs-inject based on the
  // current game-window state and handles the small race window
  // internally; the FE never decides which mutation to fire.
  function playAccount(loginId: string, acc: Account) {
    setFallbackOTP(null);
    launchAccount.mutate(
      { loginId, account: acc },
      {
        onSuccess: (result) => {
          if (result.autoFilled === false && result.otp) {
            setFallbackOTP({ loginId, sid: acc.sid, otp: result.otp });
          }
        },
        onError: (e) => handleMutationError(e, loginId),
      },
    );
  }

  function copyFallback(item: FallbackOTP) {
    navigator.clipboard
      .writeText(`${item.sid}\n${item.otp}`)
      .then(() => markCopied(cardKey(item.loginId, item.sid)))
      .catch((e) => console.error("clipboard write failed:", e));
  }

  function primaryStatusFor(loginId: string, acc: Account): PrimaryStatus {
    if (isVariablesFor(launchAccount.variables, loginId, acc)) {
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
    loginId: string,
    acc: Account,
  ): "idle" | "pending" | "error" | "copied" {
    if (copiedKey === cardKey(loginId, acc.sid)) return "copied";
    if (!isVariablesFor(fetchOTP.variables, loginId, acc)) return "idle";
    if (fetchOTP.isPending) return "pending";
    if (fetchOTP.isError) return "error";
    return "idle";
  }

  function renderAccountCard(loginId: string, acc: Account) {
    const primary = primaryStatusFor(loginId, acc);
    const copyStatus = copyStatusFor(loginId, acc);
    const fallback =
      fallbackOTP?.loginId === loginId && fallbackOTP.sid === acc.sid
        ? fallbackOTP
        : null;
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
        key={cardKey(loginId, acc.sid)}
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
          <>
            <p className="mt-2 break-words text-xs text-destructive">
              {errorText}
            </p>
            {renderGoToSettings(launchAccount.error)}
          </>
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
              {copiedKey === cardKey(fallback.loginId, fallback.sid) ? "✓" : "複製"}
            </Button>
          </div>
        )}

        <div className="mt-3 flex justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={isThisAccountPending}
            onClick={() => copyCredentials(loginId, acc)}
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
            onClick={() => playAccount(loginId, acc)}
          >
            進入遊戲
          </Button>
        </div>
      </li>
    );
  }

  function cleanSpawn() {
    setFallbackOTP(null);
    spawnGameClean.mutate(undefined, {
      onError: (e) => handleMutationError(e),
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
            disabled={logoutAll.isPending}
          >
            全部登出
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
        <div className="flex flex-col gap-1">
          <Button
            disabled={spawnGameClean.isPending || gameState.isPending}
            onClick={cleanSpawn}
          >
            {spawnGameClean.isPending ? "啟動中…" : "啟動(可切換帳號)"}
          </Button>

          {spawnGameClean.isError && (
            <>
              <p className="break-words text-xs text-destructive">
                {friendlyError(spawnGameClean.error)}
              </p>
              {renderGoToSettings(spawnGameClean.error)}
            </>
          )}
        </div>

        {notice && (
          <div className="flex items-center gap-2 rounded-md border border-amber-500/40 bg-amber-500/15 px-3 py-2 text-sm text-amber-700 dark:text-amber-300">
            <span className="min-w-0 flex-1">{notice.label} 的登入已失效</span>
            <Button
              variant="ghost"
              size="sm"
              aria-label="關閉通知"
              onClick={() => setNotice(null)}
            >
              ✕
            </Button>
          </div>
        )}

        {loginList.map((login, i) => (
          <LoginGroup
            key={login.id}
            login={login}
            isFirst={i === 0}
            lastUsedSid={lastUsedSid}
            logoutPending={logoutLogin.isPending || logoutAll.isPending}
            onLogout={logoutOne}
            renderCard={renderAccountCard}
          />
        ))}

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={loginList.length >= MAX_LOGINS}
            onClick={addLogin}
          >
            ＋ 新增 Beanfun 帳號
          </Button>
          {loginList.length >= MAX_LOGINS && (
            <span className="text-xs text-muted-foreground">
              最多同時登入 {MAX_LOGINS} 個 Beanfun 帳號
            </span>
          )}
        </div>
      </section>
    </AppShell>
  );
}
