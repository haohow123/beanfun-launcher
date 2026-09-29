import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { Service as AlertSoundService, type Sound } from "@bindings/alertsound";

export const alertSoundOptionsQueryKey = ["alertSound", "options"] as const;
export const alertSoundSelectedQueryKey = ["alertSound", "selected"] as const;
const selectAlertSoundMutationKey = ["alertSound", "select"] as const;
const previewAlertSoundMutationKey = ["alertSound", "preview"] as const;
const addCustomAlertSoundMutationKey = ["alertSound", "addCustom"] as const;
const removeCustomAlertSoundMutationKey = ["alertSound", "removeCustom"] as const;

/**
 * sameSound compares two Sound values by their identifying fields — used to
 * mark the radio row matching the current selection.
 */
export function sameSound(a: Sound, b: Sound): boolean {
  return a.kind === b.kind && a.name === b.name && a.path === b.path;
}

// refetchOnWindowFocus picks up a sound file going missing without adding a poll.
export function useAlertSoundOptionsQuery() {
  return useQuery({
    queryKey: alertSoundOptionsQueryKey,
    queryFn: () => AlertSoundService.Options(),
    refetchOnWindowFocus: true,
  });
}

/** useAlertSoundSelectedQuery reads the current selection and its Missing flag. */
export function useAlertSoundSelectedQuery() {
  return useQuery({
    queryKey: alertSoundSelectedQueryKey,
    queryFn: () => AlertSoundService.Selected(),
    refetchOnWindowFocus: true,
  });
}

/** MutationErrorOptions lets a caller receive every mutate() failure via the hook's own onError, not a per-call one. */
type MutationErrorOptions = { onError?: (err: unknown) => void };

/** useSelectAlertSoundMutation persists a new selection; select-to-save, no separate save button. */
export function useSelectAlertSoundMutation(opts?: MutationErrorOptions) {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: selectAlertSoundMutationKey,
    mutationFn: (snd: Sound) => AlertSoundService.Select(snd),
    onError: opts?.onError,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: alertSoundOptionsQueryKey });
      qc.invalidateQueries({ queryKey: alertSoundSelectedQueryKey });
    },
  });
}

/** usePreviewAlertSoundMutation plays a sound once without changing the selection. */
export function usePreviewAlertSoundMutation(opts?: MutationErrorOptions) {
  return useMutation({
    mutationKey: previewAlertSoundMutationKey,
    mutationFn: (snd: Sound) => AlertSoundService.Preview(snd),
    onError: opts?.onError,
  });
}

/** useAddCustomAlertSoundMutation validates and adds a WAV path, selecting it. */
export function useAddCustomAlertSoundMutation(opts?: MutationErrorOptions) {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: addCustomAlertSoundMutationKey,
    mutationFn: (path: string) => AlertSoundService.AddCustom(path),
    onError: opts?.onError,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: alertSoundOptionsQueryKey });
      qc.invalidateQueries({ queryKey: alertSoundSelectedQueryKey });
    },
  });
}

/** useRemoveCustomAlertSoundMutation drops a path from the list; the file on disk is untouched. */
export function useRemoveCustomAlertSoundMutation(opts?: MutationErrorOptions) {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: removeCustomAlertSoundMutationKey,
    mutationFn: (path: string) => AlertSoundService.RemoveCustom(path),
    onError: opts?.onError,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: alertSoundOptionsQueryKey });
      qc.invalidateQueries({ queryKey: alertSoundSelectedQueryKey });
    },
  });
}
