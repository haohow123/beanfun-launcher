import { useQuery } from "@tanstack/react-query";

import { LoginService, QRStatus } from "@bindings/beanfun";

export const qrMintQueryKey = ["qrMint"] as const;
export const qrStatusQueryKey = ["qrStatus"] as const;

const POLL_INTERVAL_MS = 2000;

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
 * useQRStatusQuery polls /QRLogin/CheckLoginStatus every 2 seconds
 * while `enabled` is true, stopping automatically when the status
 * lands on a terminal value (approved or expired).
 */
export function useQRStatusQuery(enabled: boolean) {
  return useQuery({
    queryKey: qrStatusQueryKey,
    queryFn: () => LoginService.CheckQRLogin(),
    enabled,
    refetchInterval: (q) => {
      const s = q.state.data;
      if (s === QRStatus.QRStatusApproved || s === QRStatus.QRStatusExpired) {
        return false;
      }
      return POLL_INTERVAL_MS;
    },
  });
}
