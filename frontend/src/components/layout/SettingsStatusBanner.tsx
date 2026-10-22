import { useQueryClient } from "@tanstack/react-query";
import { useAtom } from "jotai";

import { Button } from "@/components/ui/button";
import {
  alertSoundOptionsQueryKey,
  alertSoundSelectedQueryKey,
} from "@/queries/alertSound";
import { gamePathQueryKey } from "@/queries/gamePath";
import { loginsQueryKey } from "@/queries/logins";
import {
  settingsStatusQueryKey,
  useSettingsStatusQuery,
} from "@/queries/settings";
import { dismissedBackupPathAtom } from "@/state/settings";

type BannerContent = {
  text: string;
  retry: boolean;
  dismissible: boolean;
};

function bannerContent(
  problem: string,
  backupPath: string,
  dismissedBackup: string,
): BannerContent | null {
  if (problem === "unreadable") {
    return {
      text: "設定檔目前讀不到（可能被其他程式鎖住），畫面上是預設值，恢復前不會存檔。",
      retry: true,
      dismissible: false,
    };
  }
  if (problem === "corrupt") {
    return {
      text: "設定檔內容損壞，畫面上是預設值。下次存檔時會先把壞檔備份再重建。",
      retry: false,
      dismissible: false,
    };
  }
  if (backupPath !== "" && backupPath !== dismissedBackup) {
    return {
      text: `損壞的設定檔已備份到 ${backupPath}`,
      retry: false,
      dismissible: true,
    };
  }
  return null;
}

/** SettingsStatusBanner warns when the settings file can't be trusted, so defaults on screen aren't mistaken for saved values. */
export function SettingsStatusBanner() {
  const qc = useQueryClient();
  const { data } = useSettingsStatusQuery();
  const [dismissedBackup, setDismissedBackup] = useAtom(dismissedBackupPathAtom);

  if (!data) return null;
  const content = bannerContent(data.problem, data.backupPath, dismissedBackup);
  if (!content) return null;

  function reload() {
    for (const queryKey of [
      settingsStatusQueryKey,
      alertSoundOptionsQueryKey,
      alertSoundSelectedQueryKey,
      gamePathQueryKey,
      loginsQueryKey,
    ]) {
      qc.invalidateQueries({ queryKey });
    }
  }

  return (
    <div
      role="status"
      className="mx-6 mt-3 flex items-center gap-2 rounded-md border border-amber-500/40 bg-amber-500/15 px-3 py-2 text-sm text-amber-700 dark:text-amber-300"
    >
      <span className="min-w-0 flex-1 break-words">{content.text}</span>
      {content.retry && (
        <Button variant="ghost" size="sm" onClick={reload}>
          重新讀取
        </Button>
      )}
      {content.dismissible && (
        <Button
          variant="ghost"
          size="sm"
          aria-label="關閉通知"
          onClick={() => setDismissedBackup(data.backupPath)}
        >
          ✕
        </Button>
      )}
    </div>
  );
}
