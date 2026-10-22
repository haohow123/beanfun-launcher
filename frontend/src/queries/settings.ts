import { useQuery } from "@tanstack/react-query";

import { Service as SettingsService } from "@bindings/settings";

export const settingsStatusQueryKey = ["settings", "status"] as const;

/** useSettingsStatusQuery reports whether the settings file is unreadable or was found corrupt. */
export function useSettingsStatusQuery() {
  return useQuery({
    queryKey: settingsStatusQueryKey,
    queryFn: () => SettingsService.Status(),
    staleTime: 0,
    refetchOnWindowFocus: true,
  });
}
