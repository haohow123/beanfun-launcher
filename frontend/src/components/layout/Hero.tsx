import { type ReactNode } from "react";

import { useMapleStatusQuery } from "@/queries/mapleStatus";

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
        {action && <div className="shrink-0">{action}</div>}
      </div>
      <div className="pointer-events-none absolute inset-x-0 bottom-0 h-12 bg-gradient-to-b from-transparent to-background" />
    </section>
  );
}
