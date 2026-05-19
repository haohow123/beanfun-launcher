import { useMutation } from "@tanstack/react-query";

import { type Account } from "@bindings/beanfun";
import { LauncherService } from "@bindings/launcher";

export const spawnGameMutationKey = ["spawn-game"] as const;
export const launchMutationKey = ["launch"] as const;
export const spawnAndInjectMutationKey = ["spawn-and-inject"] as const;
export const fetchOTPMutationKey = ["fetch-otp"] as const;

/**
 * useSpawnGameMutation fires LauncherService.SpawnGame — opens the
 * configured MapleStory.exe without waiting for the login form.
 * Returns when the spawn syscall has completed; the form appearing
 * is the OS's problem.
 *
 * Driven by the top-level "啟動遊戲" button. The per-account
 * useLaunchGameMutation is the second click — once the user can
 * see the login form they click 帶入帳密 and inject runs.
 */
export function useSpawnGameMutation() {
  return useMutation({
    mutationKey: spawnGameMutationKey,
    mutationFn: () => LauncherService.SpawnGame(),
  });
}

/**
 * useLaunchGameMutation fires LauncherService.Launch — finds the
 * already-running MapleStory window and injects the account's
 * credentials. Returns:
 *
 *   - { autoFilled: true } — credentials are in, RETURN submitted.
 *   - { noWindow: true } — no game window is open; user needs to
 *     click 啟動遊戲 first.
 *   - { autoFilled: false, otp } — window found but inject failed;
 *     surface the OTP for manual paste.
 *
 * Driven by the per-account "帶入帳密" button.
 */
export function useLaunchGameMutation() {
  return useMutation({
    mutationKey: launchMutationKey,
    mutationFn: (account: Account) => LauncherService.Launch(account),
  });
}

/**
 * useSpawnAndInjectMutation fires LauncherService.SpawnAndInject —
 * the M10 1-click orchestrator. Spawns the game (or reuses an
 * existing window), waits for the form-ready caret-burst signal,
 * injects credentials, and verifies success by watching for the
 * new MapleStoryClassTW window. Returns:
 *
 *   - { autoFilled: true } — happy path; login submitted, character
 *     select reached.
 *   - { autoFilled: false, otp, failReason } — fallback; the OTP
 *     is for clipboard-paste manual login. failReason ∈
 *     "no-window" | "form-not-ready" | "inject-failed" |
 *     "no-transition".
 *
 * Driven by the per-account "啟動並帶入" button. Replaces the
 * 2-step useSpawnGameMutation + useLaunchGameMutation combo for
 * the primary launch flow; those hooks are kept around as fallback
 * surface area but no longer have a button in HomePage.
 */
export function useSpawnAndInjectMutation() {
  return useMutation({
    mutationKey: spawnAndInjectMutationKey,
    mutationFn: (account: Account) => LauncherService.SpawnAndInject(account),
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
