import { useSetAtom } from "jotai";

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

export function HomePage() {
  const setLoggedIn = useSetAtom(loggedInAtom);
  return (
    <AppShell>
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>已登入 (mock)</CardTitle>
          <CardDescription>
            Day 3+ 會在這裡顯示遊戲清單跟啟動按鈕
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col items-center gap-3">
          <Button variant="outline" onClick={() => setLoggedIn(false)}>
            登出
          </Button>
        </CardContent>
      </Card>
    </AppShell>
  );
}
