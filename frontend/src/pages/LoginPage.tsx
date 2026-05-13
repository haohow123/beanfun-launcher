import { useSetAtom } from "jotai";
import { useEffect } from "react";

import { QRStatus } from "@bindings/beanfun";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
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
          <Button onClick={() => startMut.mutate()}>重試</Button>
        </>
      );
    }
    if (statusQuery.data === QRStatus.QRStatusExpired) {
      return (
        <>
          <p className="text-sm text-destructive">QR code 已過期</p>
          <Button onClick={() => startMut.mutate()}>重新產生</Button>
        </>
      );
    }
    if (startMut.isError) {
      return (
        <>
          <p className="text-sm text-destructive">
            登入失敗:{String(startMut.error)}
          </p>
          <Button onClick={() => startMut.mutate()}>重試</Button>
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
            className="size-64 rounded-md border"
          />
          <p className="text-sm text-muted-foreground">等待手機 app 掃描…</p>
        </>
      );
    }
    return <Button onClick={() => startMut.mutate()}>登入</Button>;
  }

  return (
    <AppShell>
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>登入 Beanfun</CardTitle>
          <CardDescription>
            點下方按鈕產生 QR code,用 Beanfun! 手機 app 掃描完成登入
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col items-center gap-4">
          {renderQRFlow()}
        </CardContent>
      </Card>
    </AppShell>
  );
}
