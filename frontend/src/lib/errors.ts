// Each needle is a stable substring of a Go error message. The Go
// counterpart is named on every entry because nothing enforces this
// contract — changing the Go message silently breaks the Chinese one.
const FRIENDLY: ReadonlyArray<readonly [string, string]> = [
  // internal/beanfun/errors.go ErrIPBlocked
  ["ip temporarily blocked", "beanfun 暫時鎖定此 IP，請稍後再試"],
  // internal/launcher/service.go errGameAlreadyRunning
  ["already running", "遊戲已開啟，請稍候再試"],
  // internal/localpath/path.go ErrNotLocal
  ["not a local file", "只支援本機磁碟上的檔案"],
  // internal/alertsound/wav.go errNotWAV
  ["not a WAV file", "只支援 WAV 檔"],
  // internal/alertsound/wav.go errPathTooLong
  ["path too long", "檔案路徑太長，請把檔案移到較短的路徑"],
  // internal/alertsound/sound.go ErrUnsupported
  ["sound playback not supported", "此平台不支援播放提示音"],
  // internal/launcher/launcher.go ErrNoGamePath / ErrGameExeMissing
  ["no game path", "找不到遊戲執行檔，請到設定指定遊戲位置"],
  ["game executable not found", "找不到遊戲執行檔，請到設定指定遊戲位置"],
  // internal/launcher/launcher.go ErrGameExeUnsigned
  ["game executable is not signed by NEXON", "遊戲執行檔不是 NEXON 簽章的版本，已停止啟動"],
  // internal/launcher/launcher.go ErrGamePathNotAFile
  ["game path is not a file", "指定的遊戲位置不是檔案"],
  // internal/launcher/launcher.go ErrGamePathNotExe
  ["game path is not an exe", "遊戲位置必須是 .exe 執行檔"],
  // internal/launcher/launcher_windows.go UAC elevation cancelled by user
  ["UAC elevation cancelled", "已取消系統管理員權限確認，遊戲未啟動"],
  // internal/launcher/launcher.go ErrSpawnFailed
  ["failed to spawn game process", "遊戲啟動失敗"],
  // internal/launcher/launcher.go ErrPlatformUnsupported
  ["only supported on Windows", "此平台不支援啟動遊戲"],
];

// Warning codes from internal/launcher/gamecheck.go checkGameExe, shown by
// the settings-page Test result alongside a passing check.
const GAME_CHECK_WARNINGS: Readonly<Record<string, string>> = {
  "unexpected executable name": "檔名不是 MapleStory.exe，請確認選的是遊戲主程式",
  "game data not found": "這個資料夾裡沒有遊戲資料檔（Data），可能不是安裝目錄，啟動時可能出現 no data file",
};

/** friendlyGameCheckWarning maps a checkGameExe warning code to its Chinese copy, or the raw code if unrecognised. */
export function friendlyGameCheckWarning(code: string): string {
  return GAME_CHECK_WARNINGS[code] ?? code;
}

// GAME_CHECK_PROBLEMS maps a checkGameExe problem code to settings-page copy, since the user is already on the settings page and doesn't need a go-to-settings action.
const GAME_CHECK_PROBLEMS: Readonly<Record<string, string>> = {
  "no game path": "找不到遊戲執行檔，請按瀏覽選擇 MapleStory.exe",
  "game executable not found": "找不到遊戲執行檔，請按瀏覽選擇 MapleStory.exe",
  "not a local file": "只支援本機磁碟上的檔案",
  "game path is not a file": "指定的遊戲位置不是檔案",
  "game path is not an exe": "遊戲位置必須是 .exe 執行檔",
  "game executable is not signed by NEXON": "這個檔案不是 NEXON 簽章的 MapleStory，請選擇正版的遊戲主程式",
};

/** friendlyGameCheckProblem maps a checkGameExe problem code to settings-page copy, or the raw code if unrecognised. */
export function friendlyGameCheckProblem(code: string): string {
  return GAME_CHECK_PROBLEMS[code] ?? code;
}

/** isGamePathError reports whether err is one of checkGameExe's path-related failures, which the UI pairs with a go-to-settings action. */
export function isGamePathError(err: unknown): boolean {
  const message = err instanceof Error ? err.message : String(err);
  return (
    message.includes("no game path") ||
    message.includes("game executable not found") ||
    message.includes("not a local file") ||
    message.includes("game path is not a file") ||
    message.includes("game path is not an exe") ||
    message.includes("game executable is not signed by NEXON")
  );
}

/**
 * friendlyError maps a backend error onto Chinese copy when we recognise
 * it, and falls back to the raw message otherwise. Used by every place
 * that renders a backend failure, so the same condition reads the same
 * way wherever it surfaces.
 */
export function friendlyError(err: unknown): string {
  const message = err instanceof Error ? err.message : String(err);
  for (const [needle, friendly] of FRIENDLY) {
    if (message.includes(needle)) return friendly;
  }
  return message;
}

// Matches wails v3's vendored cfd dialog cancellation message (cfd/errors.go), not our own code.
export function isDialogCancelled(err: unknown): boolean {
  return err instanceof Error && err.message.includes("cancelled by user");
}
