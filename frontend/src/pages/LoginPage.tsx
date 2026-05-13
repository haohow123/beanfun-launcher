import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useSetAtom } from "jotai";
import { useEffect } from "react";

import { LoginService, QRStatus } from "@bindings/beanfun";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { loggedInAtom } from "@/state/auth";

const POLL_INTERVAL_MS = 2000;

export function LoginPage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const qc = useQueryClient();

  const startMut = useMutation({
    mutationFn: () => LoginService.StartQRLogin(),
    // Drop any stale status from a previous attempt so a retry after
    // expired/error starts the poll loop fresh (otherwise refetchInterval
    // sees the old terminal state and never resumes).
    onSuccess: () => qc.removeQueries({ queryKey: ["qrStatus"] }),
  });

  const statusQuery = useQuery({
    queryKey: ["qrStatus"],
    queryFn: () => LoginService.CheckQRLogin(),
    enabled: startMut.isSuccess,
    refetchInterval: (q) => {
      const s = q.state.data;
      if (s === QRStatus.QRStatusApproved || s === QRStatus.QRStatusExpired) {
        return false;
      }
      return POLL_INTERVAL_MS;
    },
  });

  useEffect(() => {
    if (statusQuery.data === QRStatus.QRStatusApproved) {
      setLoggedIn(true);
    }
  }, [statusQuery.data, setLoggedIn]);

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
          {!startMut.isPending && !startMut.isSuccess && !startMut.isError && (
            <Button onClick={() => startMut.mutate()}>登入</Button>
          )}

          {startMut.isPending && (
            <p className="text-sm text-muted-foreground">產生 QR code 中…</p>
          )}

          {startMut.isError && (
            <>
              <p className="text-sm text-destructive">
                登入失敗:{String(startMut.error)}
              </p>
              <Button onClick={() => startMut.mutate()}>重試</Button>
            </>
          )}

          {startMut.isSuccess &&
            statusQuery.data !== QRStatus.QRStatusApproved &&
            statusQuery.data !== QRStatus.QRStatusExpired &&
            !statusQuery.isError && (
              <>
                <img
                  src={`data:image/png;base64,${startMut.data.bitmapBase64}`}
                  alt="登入用 QR code"
                  className="size-64 rounded-md border"
                />
                <p className="text-sm text-muted-foreground">
                  等待手機 app 掃描…
                </p>
              </>
            )}

          {statusQuery.data === QRStatus.QRStatusExpired && (
            <>
              <p className="text-sm text-destructive">QR code 已過期</p>
              <Button onClick={() => startMut.mutate()}>重新產生</Button>
            </>
          )}

          {statusQuery.isError && (
            <>
              <p className="text-sm text-destructive">
                登入失敗:{String(statusQuery.error)}
              </p>
              <Button onClick={() => startMut.mutate()}>重試</Button>
            </>
          )}

          {statusQuery.data === QRStatus.QRStatusApproved && (
            <p className="text-sm text-foreground">登入成功,載入中…</p>
          )}
        </CardContent>
      </Card>
    </AppShell>
  );
}
