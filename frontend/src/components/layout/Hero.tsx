import { useSetAtom } from "jotai";
import { Settings, TriangleAlert } from "lucide-react";
import { type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { useAlertSoundSelectedQuery } from "@/queries/alertSound";
import { useMapleStatusQuery } from "@/queries/mapleStatus";
import { settingsOpenAtom } from "@/state/settings";

// MapleStory event banner hosted on Beanfun's CDN. Loaded at runtime
// so we don't bundle Gamania artwork into the repo; the gradient
// underneath is the fallback when the URL 404s (network down,
// banner asset rotated, etc.) — CSS multi-background renders the
// layer below when the image layer fails to load.
//
// Update by replacing the URL when MapleStory cycles its hero
// banner. Keep the gradient in step (warm palette ≈ MapleStory's
// brand), and re-check backgroundPosition below: the current art is
// left-weighted (characters + logo on the left, empty backdrop on the
// right), so it is anchored left to crop only the dead space. A
// centre-weighted banner needs that anchor changed back.
const BANNER_URL =
  "https://tw.hicdn.beanfun.com/beanfun/WebImage/20260707111628.jpg";
const BANNER_FALLBACK_GRADIENT =
  "linear-gradient(to bottom right, #fb923c, #f59e0b, #ef4444)";

/**
 * Hero is the banner-led top section shared by every page: game
 * branding on the left, optional action slot (e.g. 登出) on the right.
 *
 * The same component is rendered on LoginPage (no action — user
 * isn't logged in yet) and HomePage (action = 登出 button) so the
 * visual continuity from login → home doesn't break across the
 * boundary.
 */
// statusVisual maps the cached MapleService.ServerStatus() outcome
// to a (dot colour, label text) pair. Three states only — green
// "伺服器開啟", red "伺服器關閉中", grey "檢查中…" (covers initial
// load + the "all probes failed AND canary failed" preserve-last
// branch, both of which mean "we don't have a confident reading").
type StatusVisual = { dotClass: string; label: string };

function statusVisualFor(
  isPending: boolean,
  isError: boolean,
  online: boolean | undefined,
): StatusVisual {
  if (isPending) return { dotClass: "bg-slate-300", label: "檢查中…" };
  if (isError) return { dotClass: "bg-amber-300", label: "狀態未知" };
  if (online) return { dotClass: "bg-emerald-300", label: "伺服器開啟" };
  return { dotClass: "bg-rose-400", label: "伺服器關閉中" };
}

// Rendered by Hero itself rather than via `action`, so LoginPage gets it too.
function SettingsButton() {
  const setSettingsOpen = useSetAtom(settingsOpenAtom);
  const selected = useAlertSoundSelectedQuery();
  const missing = selected.data?.missing ?? false;
  return (
    <div className="relative">
      <Button
        variant="outline"
        size="icon-sm"
        className="bg-background/80 backdrop-blur"
        aria-label="設定"
        title={missing ? "找不到提示音檔案" : "設定"}
        onClick={() => setSettingsOpen(true)}
      >
        <Settings />
      </Button>
      {missing && (
        <TriangleAlert className="absolute -top-1 -right-1 size-3 fill-amber-400 text-amber-950" />
      )}
    </div>
  );
}

export function Hero({ action }: { action?: ReactNode }) {
  const status = useMapleStatusQuery();
  const visual = statusVisualFor(
    status.isPending,
    status.isError,
    status.data?.online,
  );

  return (
    <section
      className="relative min-h-[150px] overflow-hidden px-4 pt-5 pb-14"
      style={{
        backgroundImage: `url('${BANNER_URL}'), ${BANNER_FALLBACK_GRADIENT}`,
        backgroundSize: "cover, cover",
        backgroundPosition: "left center, center",
      }}
    >
      {/* Top dark scrim — locks text legibility against bright banner
          art. Without this the white text disappears whenever the
          banner cycles to a high-key palette. Height covers H1 +
          status line + a touch of breathing room. */}
      <div className="pointer-events-none absolute inset-x-0 top-0 h-24 bg-gradient-to-b from-black/55 via-black/25 to-transparent" />
      <div className="relative z-10 flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <h1 className="text-lg font-bold text-white [text-shadow:0_2px_6px_rgba(0,0,0,0.55)]">
            新楓之谷 MapleStory
          </h1>
          <p className="mt-0.5 flex items-center gap-1.5 text-xs text-white/95 [text-shadow:0_1px_4px_rgba(0,0,0,0.55)]">
            <span
              className={`size-1.5 shrink-0 rounded-full ${visual.dotClass}`}
            />
            <span>{visual.label}</span>
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <SettingsButton />
          {action}
        </div>
      </div>
      <div className="pointer-events-none absolute inset-x-0 bottom-0 h-12 bg-gradient-to-b from-transparent to-background" />
    </section>
  );
}
