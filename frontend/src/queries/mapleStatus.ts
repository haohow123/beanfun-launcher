import { useQuery } from "@tanstack/react-query";

import { MapleService } from "@bindings/maple";

export const mapleStatusQueryKey = ["mapleStatus"] as const;

// Frontend cache TTL. The actual TCP probe to game servers runs in
// a Go-side bgtask heartbeat every 2 min (see internal/maple); this
// hook just polls the cached value so the UI reflects backend
// updates within ~60 s. Refresh cadence halves the backend's so we
// don't see "lag past one full backend cycle" in the worst case.
const POLL_INTERVAL_MS = 60_000;

/**
 * useMapleStatusQuery exposes the backend's cached MapleStory
 * server-status probe to the Hero indicator. Calls
 * MapleService.ServerStatus() — a sync read of the latest
 * heartbeat-updated cache, not a fresh probe.
 *
 * Returns standard TanStack Query state. `data` is the Status
 * struct ({ online, lastChecked, checkedSince }); `isPending` is
 * true on first mount until the first IPC roundtrip lands.
 */
export function useMapleStatusQuery() {
  return useQuery({
    queryKey: mapleStatusQueryKey,
    queryFn: () => MapleService.ServerStatus(),
    refetchInterval: POLL_INTERVAL_MS,
    staleTime: 30_000,
  });
}
