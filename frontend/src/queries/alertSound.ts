import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { Service as AlertSoundService, type Sound } from "@bindings/alertsound";

export const alertSoundOptionsQueryKey = ["alertSound", "options"] as const;
export const alertSoundSelectedQueryKey = ["alertSound", "selected"] as const;
const selectAlertSoundMutationKey = ["alertSound", "select"] as const;
const previewAlertSoundMutationKey = ["alertSound", "preview"] as const;

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

/** useSelectAlertSoundMutation persists a new selection; select-to-save, no separate save button. */
export function useSelectAlertSoundMutation() {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: selectAlertSoundMutationKey,
    mutationFn: (snd: Sound) => AlertSoundService.Select(snd),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: alertSoundOptionsQueryKey });
      qc.invalidateQueries({ queryKey: alertSoundSelectedQueryKey });
    },
  });
}

/** usePreviewAlertSoundMutation plays a sound once without changing the selection. */
export function usePreviewAlertSoundMutation() {
  return useMutation({
    mutationKey: previewAlertSoundMutationKey,
    mutationFn: (snd: Sound) => AlertSoundService.Preview(snd),
  });
}
