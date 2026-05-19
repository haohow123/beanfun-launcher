import { useMutation } from "@tanstack/react-query";

import { type Account } from "@bindings/beanfun";
import { LauncherService } from "@bindings/launcher";

export const spawnGameMutationKey = ["spawn-game"] as const;
export const spawnGameCleanMutationKey = ["spawn-game-clean"] as const;
export const launchMutationKey = ["launch"] as const;
export const fetchOTPMutationKey = ["fetch-otp"] as const;

/**
 * useSpawnGameMutation fires LauncherService.SpawnGame(account) — the
 * M10.1 argv path. Game.exe receives the OTP at spawn time via
 * `<exe> <host> <port> BeanFun <SID> <OTP>` argv and auto-logs in to
 * character select without rendering a form.
 *
 * Driven by the per-account "啟動遊戲" button when the FE's
 * useGameStateQuery reports running=false. When running=true, the
 * button switches to "帶入帳密" + useLaunchGameMutation (M8 fallback
 * for already-open game windows that can't accept argv retroactively).
 */
export function useSpawnGameMutation() {
  return useMutation({
    mutationKey: spawnGameMutationKey,
    mutationFn: (account: Account) => LauncherService.SpawnGame(account),
  });
}

/**
 * useSpawnGameCleanMutation fires LauncherService.SpawnGameClean() —
 * spawns MapleStory.exe with no argv so the login form renders. Used
 * by the multi-account flow: open the game once, switch accounts
 * mid-play via the in-game "back to login" → 帶入帳密 each time.
 * The argv-based useSpawnGameMutation can't support this because
 * its OTP is single-use and the game refuses to return to login
 * after consuming it.
 *
 * Driven by the standalone 「啟動(可切換帳號)」 button under the Hero
 * area in HomePage, visible only when gameState.running is false.
 */
export function useSpawnGameCleanMutation() {
  return useMutation({
    mutationKey: spawnGameCleanMutationKey,
    mutationFn: () => LauncherService.SpawnGameClean(),
  });
}

/**
 * useLaunchGameMutation fires LauncherService.Launch(account) — the
 * M8 WM_CHAR fallback path. Finds the already-running MapleStory window
 * (no spawn) and types credentials into its login form via PostMessage.
 *
 * Returns:
 *   - { autoFilled: true } — credentials are in, RETURN submitted.
 *   - { noWindow: true } — game window vanished between state-changed
 *     event and mutation fire (rare race). FE should re-query GameState.
 *   - { autoFilled: false, otp } — window found but inject failed
 *     mid-sequence; surface OTP for manual paste.
 *
 * Driven by the per-account "帶入帳密" button when useGameStateQuery
 * reports running=true (game opened externally, or our argv spawn
 * preceded launcher restart).
 */
export function useLaunchGameMutation() {
  return useMutation({
    mutationKey: launchMutationKey,
    mutationFn: (account: Account) => LauncherService.Launch(account),
  });
}

/**
 * useFetchOTPMutation calls LauncherService.GetOTP — fetches the OTP
 * without spawning the game. Used for: macOS dev verification (no
 * Windows runtime), users who launch via their own tooling.
 *
 * The OTP rotates on each call and is single-use server-side, so
 * holding it in JS state until the user pastes is acceptable.
 */
export function useFetchOTPMutation() {
  return useMutation({
    mutationKey: fetchOTPMutationKey,
    mutationFn: (account: Account) => LauncherService.GetOTP(account),
  });
}
