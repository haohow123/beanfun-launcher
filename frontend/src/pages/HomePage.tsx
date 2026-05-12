import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

interface HomePageProps {
  onLogout: () => void;
}

export function HomePage({ onLogout }: HomePageProps) {
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
          <Button variant="outline" onClick={onLogout}>
            登出
          </Button>
        </CardContent>
      </Card>
    </AppShell>
  );
}
