import { useMutation } from "@tanstack/react-query";

import { type Account } from "@bindings/beanfun";
import { LauncherService } from "@bindings/launcher";

export const launchMutationKey = ["launch"] as const;

/**
 * useLaunchGameMutation fires LauncherService.Launch with the given
 * Account. The backend handles the 6-step OTP fetch + game.exe spawn
 * + token zeroing; this hook just surfaces pending / error / success
 * state.
 *
 * The mutation is fire-and-forget from React's perspective — there's
 * no return value beyond "did the spawn syscall succeed?". Game
 * lifetime (whether the user closed the window, whether it crashed)
 * is the OS's problem, not ours.
 */
export function useLaunchGameMutation() {
  return useMutation({
    mutationKey: launchMutationKey,
    mutationFn: (account: Account) => LauncherService.Launch(account),
  });
}
