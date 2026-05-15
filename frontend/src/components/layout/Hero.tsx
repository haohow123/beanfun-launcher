import { type ReactNode } from "react";

// MapleStory event banner hosted on Beanfun's CDN. Loaded at runtime
// so we don't bundle Gamania artwork into the repo; the gradient
// underneath is the fallback when the URL 404s (network down,
// banner asset rotated, etc.) — CSS multi-background renders the
// layer below when the image layer fails to load.
//
// Update by replacing the URL when MapleStory cycles its hero
// banner. Keep the gradient in step (warm palette ≈ MapleStory's
// brand).
const BANNER_URL =
  "https://tw.hicdn.beanfun.com/beanfun/WebImage/20260410030136.jpg";
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
export function Hero({ action }: { action?: ReactNode }) {
  return (
    <section
      className="relative min-h-[240px] overflow-hidden px-4 pt-5 pb-16"
      style={{
        backgroundImage: `url('${BANNER_URL}'), ${BANNER_FALLBACK_GRADIENT}`,
        backgroundSize: "cover, cover",
        backgroundPosition: "center, center",
      }}
    >
      <div className="relative z-10 flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <h1 className="text-lg font-bold text-white [text-shadow:0_1px_3px_rgba(0,0,0,0.45)]">
            新楓之谷 MapleStory
          </h1>
          <p className="mt-0.5 text-xs text-white/90 [text-shadow:0_1px_2px_rgba(0,0,0,0.4)]">
            <span className="inline-block size-1.5 rounded-full bg-emerald-300 align-middle" />{" "}
            伺服器狀態 (TODO)
          </p>
        </div>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      <div className="pointer-events-none absolute inset-x-0 bottom-0 h-12 bg-gradient-to-b from-transparent to-background" />
    </section>
  );
}
