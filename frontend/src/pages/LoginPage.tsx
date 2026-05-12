import { useEffect } from "react";

import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useQRPolling } from "@/hooks/useQRPolling";

interface LoginPageProps {
  onApproved: () => void;
}

export function LoginPage({ onApproved }: LoginPageProps) {
  const { state, start } = useQRPolling();

  // Lift approval up to the App router so it can swap pages.
  useEffect(() => {
    if (state.kind === "approved") {
      onApproved();
    }
  }, [state, onApproved]);

  return (
    <AppShell>
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>登入 Beanfun</CardTitle>
          <CardDescription>
            點下方按鈕產生 QR code，用 Beanfun! 手機 app 掃描完成登入
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col items-center gap-4">
          {state.kind === "idle" && (
            <Button onClick={() => start()}>登入</Button>
          )}
          {state.kind === "starting" && (
            <p className="text-sm text-muted-foreground">
              產生 QR code 中…
            </p>
          )}
          {state.kind === "polling" && (
            <>
              <img
                src={`data:image/png;base64,${state.qr.bitmapBase64}`}
                alt="登入用 QR code"
                className="size-64 rounded-md border"
              />
              <p className="text-sm text-muted-foreground">
                等待手機 app 掃描…
              </p>
            </>
          )}
          {state.kind === "expired" && (
            <>
              <p className="text-sm text-destructive">QR code 已過期</p>
              <Button onClick={() => start()}>重新產生</Button>
            </>
          )}
          {state.kind === "error" && (
            <>
              <p className="text-sm text-destructive">
                登入失敗：{state.message}
              </p>
              <Button onClick={() => start()}>重試</Button>
            </>
          )}
          {state.kind === "approved" && (
            <p className="text-sm text-foreground">登入成功，載入中…</p>
          )}
        </CardContent>
      </Card>
    </AppShell>
  );
}
