import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";

import { MapleService, Status } from "@bindings/maple";

export const mapleStatusQueryKey = ["mapleStatus"] as const;

// Must match maple.StatusChangedEvent in internal/maple/service.go.
const STATUS_CHANGED_EVENT = "maple:status-changed";

/**
 * useMapleStatusQuery exposes the backend's cached MapleStory
 * server-status probe to the Hero indicator. Calls
 * MapleService.ServerStatus() — a sync read of the latest
 * heartbeat-updated cache, not a fresh probe.
 *
 * The read happens once on mount; every later change arrives through
 * useMapleStatusEventBridge below. The pull is kept because the very
 * first emit can be dropped: the Go-side probe runs with firstDelay=0
 * and may complete before the webview runtime is ready to receive.
 *
 * Returns standard TanStack Query state. `data` is the Status
 * struct ({ online, lastChecked, checkedSince }); `isPending` is
 * true on first mount until the first IPC roundtrip lands.
 */
export function useMapleStatusQuery() {
  return useQuery({
    queryKey: mapleStatusQueryKey,
    queryFn: () => MapleService.ServerStatus(),
    staleTime: Infinity,
    // A sleeping machine leaves the 2-min backend heartbeat un-run, so
    // on wake the cache can be a full interval stale with no event to
    // correct it. Overrides the global refetchOnWindowFocus: false.
    refetchOnWindowFocus: true,
  });
}

/**
 * Subscribes to the backend's "maple:status-changed" Wails event and
 * funnels payloads into the mapleStatusQueryKey cache. Call this once
 * at the App root.
 *
 * The Go side emits on the initial probe and on every Online flip in
 * either direction — not on every probe, since LastChecked advances
 * each time and nothing reads it.
 */
export function useMapleStatusEventBridge() {
  const qc = useQueryClient();
  useEffect(() => {
    return Events.On(STATUS_CHANGED_EVENT, (e) => {
      // Annotated because setQueryData is unconstrained here — the query
      // key carries no DataTag, so tsc would accept any value.
      const next: Status = Status.createFrom(e.data);
      qc.setQueryData(mapleStatusQueryKey, next);
    });
  }, [qc]);
}
