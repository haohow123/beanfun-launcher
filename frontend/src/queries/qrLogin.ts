import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import { LoginService, QRStatus } from "@bindings/beanfun";

export const qrStatusQueryKey = ["qrStatus"] as const;

const POLL_INTERVAL_MS = 2000;

/**
 * useStartQRLoginMutation kicks off the QR-login flow (one-shot,
 * user-triggered). On success it clears any stale ['qrStatus'] cache
 * so the poll loop starts from a clean slate — otherwise a retry
 * after expired/error sees the prior terminal state and never resumes.
 */
export function useStartQRLoginMutation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => LoginService.StartQRLogin(),
    onSuccess: () => qc.removeQueries({ queryKey: qrStatusQueryKey }),
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
