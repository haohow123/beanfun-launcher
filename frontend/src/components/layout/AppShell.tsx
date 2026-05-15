import { type ReactNode } from "react";

import { cn } from "@/lib/utils";

/**
 * AppShell is the chrome around every page. The 50 px header strip
 * matches the `InvisibleTitleBarHeight: 50` declared in main.go for the
 * macOS frameless window so the traffic lights live there instead of
 * eating page content; the whole strip is draggable via
 * `-webkit-app-region: drag` (see `.app-titlebar` in index.css).
 *
 * `mainClassName` overrides the centred default — LoginPage keeps the
 * centred QR card; HomePage passes `flex-col` so its banner / hero
 * layout can stretch top-to-bottom without first being shoved to the
 * middle.
 */
export function AppShell({
  children,
  mainClassName = "items-center justify-center p-6",
}: {
  children: ReactNode;
  mainClassName?: string;
}) {
  return (
    <div className="flex min-h-screen flex-col bg-background">
      <header className="app-titlebar flex h-[50px] shrink-0 items-center pl-24 pr-4">
        <span className="text-xs font-medium text-muted-foreground">
          Beanfun Launcher
        </span>
      </header>
      <main className={cn("flex flex-1", mainClassName)}>{children}</main>
    </div>
  );
}
