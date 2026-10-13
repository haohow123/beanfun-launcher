import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { LauncherService } from "@bindings/launcher";

export const gamePathQueryKey = ["gamePath"] as const;
const setGamePathMutationKey = ["gamePath", "set"] as const;
const testGamePathMutationKey = ["gamePath", "test"] as const;

type MutationErrorOptions = { onError?: (err: unknown) => void };

/** useGamePathQuery reads the settings-page path, the registry detection, and the effective check. */
export function useGamePathQuery() {
  return useQuery({
    queryKey: gamePathQueryKey,
    queryFn: () => LauncherService.GamePath(),
    refetchOnWindowFocus: true,
  });
}

/** useSetGamePathMutation persists the settings-page path, where an empty string clears it back to auto-detect. */
export function useSetGamePathMutation(opts?: MutationErrorOptions) {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: setGamePathMutationKey,
    mutationFn: (path: string) => LauncherService.SetGamePath(path),
    onError: opts?.onError,
    onSuccess: () => {
      return qc.invalidateQueries({ queryKey: gamePathQueryKey });
    },
  });
}

/** useTestGamePathMutation runs the settings-page Test check against a candidate path, or the effective path when empty. */
export function useTestGamePathMutation() {
  return useMutation({
    mutationKey: testGamePathMutationKey,
    mutationFn: (path: string) => LauncherService.TestGamePath(path),
  });
}
