import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";

import { LoginService, QRState } from "@bindings/beanfun";

export const qrMintQueryKey = ["qrMint"] as const;
export const qrStatusQueryKey = ["qrStatus"] as const;

// Must match beanfun.QRStateChangedEvent in internal/beanfun/service.go.
const QR_STATE_CHANGED_EVENT = "beanfun:qr-state-changed";

/**
 * useQRMintQuery fetches a fresh QR + deeplink from Beanfun. Runs
 * automatically on mount; refresh by calling .refetch() (or
 * queryClient.invalidateQueries).
 *
 * Why useQuery instead of useMutation: under React StrictMode's dev
 * double-invoke, a `useMutation` called from `useEffect` on mount has
 * its observer torn down between the synthetic unmount and remount.
 * The Wails IPC response then resolves into the discarded observer
 * and the UI stays in `pending` forever — observed in alpha.25 as the
 * "卡在產生 QR code 中…" symptom where the backend logged a clean
 * `StartQRLogin: returning to frontend` but the frontend never saw
 * the result. Queries are keyed by `queryKey` on the QueryClient
 * cache, so the resubscribed observer reads the result out of cache
 * — safe across the double-invoke.
 *
 * staleTime / gcTime: Infinity so the query never auto-refetches on
 * focus / interval. User-initiated refresh goes through .refetch().
 */
export function useQRMintQuery() {
  return useQuery({
    queryKey: qrMintQueryKey,
    queryFn: () => LoginService.StartQRLogin(),
    staleTime: Infinity,
    gcTime: Infinity,
    retry: 1,
  });
}

/**
 * useQRStatusQuery reads the backend's cached QR-login state once, then
 * receives every later change through useQRStateEventBridge below.
 *
 * The read is kept rather than relying on the push alone: an emit is
 * dropped outright when the window has no impl yet, and again when the
 * frontend has not registered its listener, so the first state change
 * can arrive before anyone is listening.
 */
export function useQRStatusQuery(enabled: boolean) {
  return useQuery({
    queryKey: qrStatusQueryKey,
    queryFn: () => LoginService.QRStatusNow(),
    enabled,
    staleTime: Infinity,
  });
}

/**
 * useQRStateEventBridge funnels the backend's QR state pushes into the
 * qrStatusQueryKey cache. Call this once at the App root so the
 * subscription outlives the login → home navigation.
 */
export function useQRStateEventBridge() {
  const qc = useQueryClient();
  useEffect(() => {
    return Events.On(QR_STATE_CHANGED_EVENT, (e) => {
      // Annotated because setQueryData is unconstrained on a key with no DataTag.
      const next: QRState = QRState.createFrom(e.data);
      qc.setQueryData(qrStatusQueryKey, next);
    });
  }, [qc]);
}
