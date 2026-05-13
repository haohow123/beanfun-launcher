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
} from "@/queries/launch";
import { loggedInAtom } from "@/state/auth";

export function HomePage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  const qc = useQueryClient();
  const accounts = useAccountsQuery();
  const launch = useLaunchGameMutation();
  const fetchOTP = useFetchOTPMutation();
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  function logout() {
    qc.clear();
    setLoggedIn(false);
  }

  function retry() {
    accounts.refetch();
  }

  function launchStatusFor(acc: Account): "idle" | "pending" | "error" | "success" {
    if (launch.variables?.sid !== acc.sid) return "idle";
    if (launch.isPending) return "pending";
    if (launch.isError) return "error";
    if (launch.isSuccess) return "success";
    return "idle";
  }

  function otpStatusFor(acc: Account): "idle" | "pending" | "error" | "ready" {
    if (fetchOTP.variables?.sid !== acc.sid) return "idle";
    if (fetchOTP.isPending) return "pending";
    if (fetchOTP.isError) return "error";
    if (fetchOTP.isSuccess) return "ready";
    return "idle";
  }

  async function copyValue(key: string, value: string) {
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      setCopiedKey(key);
      window.setTimeout(
        () => setCopiedKey((k) => (k === key ? null : k)),
        1500,
      );
    } catch {
      console.error("clipboard write failed");
    }
  }

  function renderAccountItem(acc: Account) {
    const launchStatus = launchStatusFor(acc);
    const otpStatus = otpStatusFor(acc);
    const otpValue = otpStatus === "ready" ? (fetchOTP.data ?? "") : "";

    return (
      <li key={acc.sid} className="rounded-md border p-3">
        <div className="mb-2 flex items-baseline justify-between gap-2">
          <span className="text-sm font-medium">{acc.sname}</span>
          {launchStatus === "pending" && (
            <span className="text-xs text-muted-foreground">啟動中…</span>
          )}
          {launchStatus === "success" && (
            <span className="text-xs text-foreground">✓ 已啟動</span>
          )}
          {launchStatus === "error" && (
            <span
              className="text-xs text-destructive"
              title={String(launch.error)}
            >
              啟動失敗
            </span>
          )}
        </div>

        <div className="flex flex-col gap-2">
          <CredentialRow
            label="帳號"
            value={acc.sid}
            copied={copiedKey === `${acc.sid}:sid`}
            onCopy={() => copyValue(`${acc.sid}:sid`, acc.sid)}
          />

          <CredentialRow
            label="密碼"
            value={otpValue}
            placeholder={
              otpStatus === "pending"
                ? "產生中…"
                : otpStatus === "error"
                  ? `失敗:${String(fetchOTP.error)}`
                  : "尚未產生"
            }
            copied={copiedKey === `${acc.sid}:otp`}
            onCopy={() => copyValue(`${acc.sid}:otp`, otpValue)}
            extraAction={
              <Button
                variant="outline"
                size="sm"
                disabled={otpStatus === "pending"}
                onClick={() => fetchOTP.mutate(acc)}
              >
                {otpStatus === "pending" ? "…" : "產生 OTP"}
              </Button>
            }
          />
        </div>

        <div className="mt-3 flex justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={!otpValue}
            onClick={() => copyValue(`${acc.sid}:both`, `${acc.sid}\n${otpValue}`)}
            title="把帳號+密碼一起複製,中間用換行隔開"
          >
            {copiedKey === `${acc.sid}:both` ? "✓ 已複製" : "複製帳密"}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={launchStatus === "pending"}
            onClick={() => launch.mutate(acc)}
          >
            啟動遊戲
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
        {accounts.data.map(renderAccountItem)}
      </ul>
    );
  }

  return (
    <AppShell>
      <Card className="w-full max-w-lg">
        <CardHeader>
          <CardTitle>遊戲帳號</CardTitle>
          <CardDescription>
            點「啟動遊戲」直接開,或「產生 OTP」貼到其他啟動器
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
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

interface CredentialRowProps {
  label: string;
  value: string;
  placeholder?: string;
  copied: boolean;
  onCopy: () => void;
  extraAction?: React.ReactNode;
}

function CredentialRow({
  label,
  value,
  placeholder,
  copied,
  onCopy,
  extraAction,
}: CredentialRowProps) {
  return (
    <div className="flex items-center gap-2">
      <span className="w-10 shrink-0 text-xs text-muted-foreground">
        {label}
      </span>
      <input
        type="text"
        readOnly
        value={value}
        placeholder={placeholder}
        onFocus={(e) => e.currentTarget.select()}
        className="flex-1 rounded-md border bg-background px-2 py-1 font-mono text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
      />
      {extraAction}
      <Button
        variant="outline"
        size="sm"
        disabled={!value}
        onClick={onCopy}
      >
        {copied ? "✓" : "複製"}
      </Button>
    </div>
  );
}
