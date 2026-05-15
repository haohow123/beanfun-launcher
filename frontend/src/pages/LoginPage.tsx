import { useSetAtom } from "jotai";
import { useEffect, useRef } from "react";

import { QRStatus } from "@bindings/beanfun";
import { AppShell } from "@/components/layout/AppShell";
import { Hero } from "@/components/layout/Hero";
import { Button } from "@/components/ui/button";
import {
  useQRStatusQuery,
  useStartQRLoginMutation,
} from "@/queries/qrLogin";
import { loggedInAtom } from "@/state/auth";

export function LoginPage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const startMut = useStartQRLoginMutation();
  const statusQuery = useQRStatusQuery(startMut.isSuccess);

  useEffect(() => {
    if (statusQuery.data === QRStatus.QRStatusApproved) {
      setLoggedIn(true);
    }
  }, [statusQuery.data, setLoggedIn]);

  // Auto-fire the QR mint on mount — login is the only thing the
  // user can do on this page, so a "click 登入 to start" button is
  // an extra hop with no choice attached. Re-mount (after logout
  // or session expiry) re-fires because the ref resets.
  //
  // useRef guard rather than `[]` deps so React StrictMode's dev
  // double-invoke doesn't fire the mutation twice (which would
  // mint two QR codes and orphan the first cookie jar).
  const autoStarted = useRef(false);
  useEffect(() => {
    if (autoStarted.current) return;
    autoStarted.current = true;
    startMut.mutate();
  }, [startMut]);

  // Wrapper exists because startMut.mutate's signature is
  // (variables: void, ...) => void, which TS won't accept as an
  // onClick handler (MouseEvent isn't void). Still used by the
  // 重試 / 重新產生 buttons after a failure / expiry.
  function startLogin() {
    startMut.mutate();
  }

  // Ordered by priority: terminal status states first, then mutation
  // states, then the polling display, then the initial idle button.
  // Each branch checks one thing — no `!a && !b && !c && ...` pile-up.
  function renderQRFlow() {
    if (statusQuery.data === QRStatus.QRStatusApproved) {
      return <p className="text-sm text-foreground">登入成功,載入中…</p>;
    }
    if (statusQuery.isError) {
      return (
        <>
          <p className="text-sm text-destructive">
            登入失敗:{String(statusQuery.error)}
          </p>
          <Button onClick={startLogin}>重試</Button>
        </>
      );
    }
    if (statusQuery.data === QRStatus.QRStatusExpired) {
      return (
        <>
          <p className="text-sm text-destructive">QR code 已過期</p>
          <Button onClick={startLogin}>重新產生</Button>
        </>
      );
    }
    if (startMut.isError) {
      return (
        <>
          <p className="text-sm text-destructive">
            登入失敗:{String(startMut.error)}
          </p>
          <Button onClick={startLogin}>重試</Button>
        </>
      );
    }
    if (startMut.isPending) {
      return <p className="text-sm text-muted-foreground">產生 QR code 中…</p>;
    }
    if (startMut.isSuccess) {
      return (
        <>
          <img
            src={`data:image/png;base64,${startMut.data.bitmapBase64}`}
            alt="登入用 QR code"
            className="size-56 rounded-md border"
          />
          <p className="text-sm text-muted-foreground">等待手機 app 掃描…</p>
        </>
      );
    }
    // The auto-start useEffect above runs after first paint, so the
    // very first render reaches here before mutation has flipped to
    // pending. Render the pending copy so the user never sees a
    // blank flash.
    return <p className="text-sm text-muted-foreground">產生 QR code 中…</p>;
  }

  return (
    <AppShell mainClassName="flex-col">
      <Hero />

      <section className="flex flex-1 flex-col items-center gap-4 px-4 py-6">
        <div className="text-center">
          <h2 className="text-base font-semibold">登入 Beanfun</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            點下方按鈕產生 QR code,用 Beanfun! 手機 app 掃描完成登入
          </p>
        </div>
        {renderQRFlow()}
      </section>
    </AppShell>
  );
}
