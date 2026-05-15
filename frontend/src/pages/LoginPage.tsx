import { useQueryClient } from "@tanstack/react-query";
import { useSetAtom } from "jotai";
import { useEffect } from "react";

import { QRStatus } from "@bindings/beanfun";
import { AppShell } from "@/components/layout/AppShell";
import { Hero } from "@/components/layout/Hero";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  qrStatusQueryKey,
  useQRMintQuery,
  useQRStatusQuery,
} from "@/queries/qrLogin";
import { loggedInAtom } from "@/state/auth";

export function LoginPage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const qc = useQueryClient();
  const qrMint = useQRMintQuery();
  const statusQuery = useQRStatusQuery(qrMint.isSuccess);

  useEffect(() => {
    if (statusQuery.data === QRStatus.QRStatusApproved) {
      setLoggedIn(true);
    }
  }, [statusQuery.data, setLoggedIn]);

  // Refresh = re-fire the mint + drop any in-flight poll state so
  // the next /CheckLoginStatus call sees the fresh pendingQR.
  function regenerate() {
    qc.removeQueries({ queryKey: qrStatusQueryKey });
    qrMint.refetch();
  }

  const expired = statusQuery.data === QRStatus.QRStatusExpired;
  const approved = statusQuery.data === QRStatus.QRStatusApproved;
  const minting = qrMint.isFetching;
  const hasQR = qrMint.isSuccess && !!qrMint.data?.bitmapBase64;

  // QR tile: skeleton while minting (visibly alive — a static
  // "產生 QR code 中…" text reads as "frozen UI" if the round-trip
  // stalls; an animated box keeps the signal that work is in
  // flight), the live QR once minted (dimmed when expired), or an
  // error placeholder on failure.
  function renderQR() {
    if (minting) {
      return (
        <div className="size-56 animate-pulse rounded-md border bg-muted" />
      );
    }
    if (qrMint.isError) {
      return (
        <div className="flex size-56 items-center justify-center rounded-md border bg-muted/40 text-sm text-destructive">
          產生失敗
        </div>
      );
    }
    if (hasQR) {
      return (
        <img
          src={`data:image/png;base64,${qrMint.data!.bitmapBase64}`}
          alt="登入 QR code"
          className={cn(
            "size-56 rounded-md border",
            expired && "opacity-30",
          )}
        />
      );
    }
    return <div className="size-56 animate-pulse rounded-md border bg-muted" />;
  }

  function renderStatus() {
    if (approved) {
      return <p className="text-sm text-foreground">登入成功,載入中…</p>;
    }
    if (expired) {
      return (
        <p className="text-sm text-amber-700 dark:text-amber-300">
          QR code 已過期,請按下方按鈕重新產生
        </p>
      );
    }
    if (statusQuery.isError) {
      return (
        <p className="break-words text-sm text-destructive">
          登入失敗:{String(statusQuery.error)}
        </p>
      );
    }
    if (qrMint.isError) {
      return (
        <p className="break-words text-sm text-destructive">
          產生失敗:{String(qrMint.error)}
        </p>
      );
    }
    if (minting) {
      return <p className="text-sm text-muted-foreground">產生 QR code 中…</p>;
    }
    if (hasQR) {
      return (
        <p className="text-sm text-muted-foreground">
          用 Beanfun 手機 app 掃描 QR code 完成登入
        </p>
      );
    }
    return <p className="text-sm text-muted-foreground">準備中…</p>;
  }

  return (
    <AppShell mainClassName="flex-col">
      <Hero />

      <section className="flex flex-1 flex-col items-center gap-3 px-4 py-6">
        <h2 className="text-base font-semibold">登入 Beanfun</h2>
        {renderQR()}
        {renderStatus()}
        <Button
          variant="outline"
          size="sm"
          onClick={regenerate}
          disabled={minting || approved}
        >
          重新產生 QR code
        </Button>
      </section>
    </AppShell>
  );
}
