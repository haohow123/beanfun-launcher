import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";

import { LauncherService } from "@bindings/launcher";

export const gameStateQueryKey = ["game-state"] as const;

/**
 * Single source of truth for the FE's view of whether MapleStory is
 * running. Backed by:
 *
 *   - initial pull: LauncherService.GetGameState() — calls findGameWindowFn
 *     once on mount so a launcher restart while game is already running
 *     reflects accurately.
 *   - push updates: the Wails "game:state-changed" event, wired via
 *     useGameStateEventBridge below.
 *
 * staleTime is Infinity so React Query never auto-refetches — every
 * transition arrives through the event bridge instead. The HomePage
 * smart button derives its label + action from data.running:
 *
 *   running=false → 啟動遊戲 → useSpawnGameMutation
 *   running=true  → 帶入帳密 → useLaunchGameMutation
 */
export function useGameStateQuery() {
  return useQuery({
    queryKey: gameStateQueryKey,
    queryFn: () => LauncherService.GetGameState(),
    staleTime: Infinity,
    // External transitions (game opened/closed outside the launcher)
    // don't fire game:state-changed — the event only covers our own
    // SpawnGame + watcher path. A re-probe whenever the launcher
    // regains window focus catches those missed updates. Overrides
    // the global queryClient default (refetchOnWindowFocus: false)
    // since this query is specifically the source of truth for the
    // smart-button affordance.
    refetchOnWindowFocus: true,
  });
}

/**
 * Subscribes to the backend's "game:state-changed" Wails event and
 * funnels payloads into the gameStateQueryKey cache. Call this once
 * at the App root — multiple call sites would each register a
 * listener and write the same value, which is harmless but wasteful.
 *
 * The event payload is the Go-side GameState struct (json-serialized):
 *   { running: boolean, hwnd?: number }
 *
 * Emit points in the backend:
 *   - SpawnGame after spawnFn returns: { running: true } (optimistic,
 *     hwnd not known until watcher Phase 1 sees the window)
 *   - watcher Phase 1 timeout: { running: false } (game never appeared)
 *   - watcher Phase 2 process exit: { running: false }
 */
export function useGameStateEventBridge() {
  const qc = useQueryClient();
  useEffect(() => {
    return Events.On("game:state-changed", (e) => {
      qc.setQueryData(gameStateQueryKey, e.data);
    });
  }, [qc]);
}
