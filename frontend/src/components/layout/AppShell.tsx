import { type ReactNode } from "react";

import { cn } from "@/lib/utils";

// macOS-only: the 50 px header band hosts the frameless window's
// drag region (and the traffic-light cut-out via pl-24). On Windows
// the native title bar already handles drag + window controls, so
// the band is pure dead vertical space — hide it. navigator.platform
// is deprecated-but-works in WebView2 / WKWebView, and a single
// boolean is cheaper than threading platform info through props.
const isMacOS =
  typeof navigator !== "undefined" && /Mac/i.test(navigator.platform);

/**
 * AppShell is the chrome around every page. On macOS the 50 px
 * header strip matches the `InvisibleTitleBarHeight: 50` declared
 * in main.go for the frameless window so the traffic lights live
 * there instead of eating page content; the whole strip is
 * draggable via `-webkit-app-region: drag` (see `.app-titlebar`
 * in index.css). On Windows the header is skipped entirely — the
 * native title bar takes care of drag + window controls.
 *
 * `mainClassName` overrides the centred default. Both pages pass
 * `flex-col` so the hero sits flush at the top. LoginPage then centres
 * its own body section (fixed-height QR card, so the slack is worth
 * splitting); HomePage stacks from the top because its account list
 * grows downward.
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
      {isMacOS && (
        <header className="app-titlebar flex h-[50px] shrink-0 items-center pl-24 pr-4">
          <span className="text-xs font-medium text-muted-foreground">
            Beanfun Launcher
          </span>
        </header>
      )}
      <main className={cn("flex flex-1", mainClassName)}>{children}</main>
    </div>
  );
}
