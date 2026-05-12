import { type ReactNode } from "react";

/**
 * AppShell is the chrome around every page. The 50 px header strip
 * matches the `InvisibleTitleBarHeight: 50` declared in main.go for the
 * macOS frameless window so the traffic lights live there instead of
 * eating page content; the whole strip is draggable via
 * `-webkit-app-region: drag` (see `.app-titlebar` in index.css).
 *
 * Bento-style multi-tile pages should still nest a Grid container
 * inside `children`; the shell itself only handles the outer column
 * (header + flex-centred main).
 */
export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-screen flex-col bg-background">
      <header className="app-titlebar flex h-[50px] shrink-0 items-center px-4">
        <span className="text-xs font-medium text-muted-foreground">
          Beanfun Launcher
        </span>
      </header>
      <main className="flex flex-1 items-center justify-center p-6">
        {children}
      </main>
    </div>
  );
}
