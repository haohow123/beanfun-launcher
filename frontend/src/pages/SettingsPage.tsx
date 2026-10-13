import { Dialogs } from "@wailsio/runtime";
import { useSetAtom } from "jotai";
import { Play, Plus, TriangleAlert, X } from "lucide-react";
import { type FocusEvent, type ReactNode, useRef, useState } from "react";

import { type Option, type Sound } from "@bindings/alertsound";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  friendlyError,
  friendlyGameCheckProblem,
  friendlyGameCheckWarning,
  isDialogCancelled,
} from "@/lib/errors";
import {
  noteDialogCancelled,
  sameSound,
  useAddCustomAlertSoundMutation,
  useAlertSoundOptionsQuery,
  useAlertSoundSelectedQuery,
  usePreviewAlertSoundMutation,
  useRemoveCustomAlertSoundMutation,
  useSelectAlertSoundMutation,
} from "@/queries/alertSound";
import {
  useGamePathQuery,
  useSetGamePathMutation,
  useTestGamePathMutation,
} from "@/queries/gamePath";
import { settingsOpenAtom } from "@/state/settings";

// GROUP_HEADING_ID links the radiogroup wrapper to the visible h2 heading it names.
const GROUP_HEADING_ID = "alert-sound-group-heading";

// "none" is deliberately absent so 無聲 renders without a heading.
const GROUP_LABELS: Record<string, string> = {
  windows: "Windows",
  alarm: "鬧鐘",
  ring: "鈴聲",
  other: "其他",
  custom: "我的音檔",
};

export function SettingsPage() {
  const setSettingsOpen = useSetAtom(settingsOpenAtom);
  const options = useAlertSoundOptionsQuery();
  const selected = useAlertSoundSelectedQuery();
  const [lastError, setLastError] = useState<string>();
  const onMutationError = (err: unknown) => setLastError(friendlyError(err));
  const selectMutation = useSelectAlertSoundMutation({ onError: onMutationError });
  const previewMutation = usePreviewAlertSoundMutation({ onError: onMutationError });
  const addCustomMutation = useAddCustomAlertSoundMutation({ onError: onMutationError });
  const removeCustomMutation = useRemoveCustomAlertSoundMutation({ onError: onMutationError });

  const gamePath = useGamePathQuery();
  const gamePathSectionRef = useRef<HTMLElement>(null);
  const [gamePathInput, setGamePathInput] = useState<string>();
  const [gamePathError, setGamePathError] = useState<string>();
  const savedGamePath = gamePath.data?.setting ?? "";
  const gamePathValue = gamePathInput ?? savedGamePath;
  const isEnvGamePath = gamePath.data?.source === "env";
  const onGamePathError = (err: unknown) => setGamePathError(friendlyError(err));
  const setGamePathMutation = useSetGamePathMutation({ onError: onGamePathError });
  const testGamePathMutation = useTestGamePathMutation();

  // Flushes a pending edit before navigating away so a save failure surfaces here, not after unmount.
  async function goBack() {
    const trimmed = gamePathValue.trim();
    if (trimmed !== savedGamePath) {
      setGamePathError(undefined);
      try {
        await setGamePathMutation.mutateAsync(trimmed);
        setGamePathInput(undefined);
      } catch (err) {
        setGamePathError(friendlyError(err));
        return;
      }
    }
    setSettingsOpen(false);
  }

  function selectSound(snd: Sound) {
    setLastError(undefined);
    selectMutation.mutate(snd);
  }

  function previewSound(snd: Sound) {
    setLastError(undefined);
    previewMutation.mutate(snd);
  }

  function removeCustomSound(path: string) {
    setLastError(undefined);
    removeCustomMutation.mutate(path);
  }

  async function handleAddSound() {
    setLastError(undefined);
    let path: string;
    try {
      path = await Dialogs.OpenFile({
        Title: "選擇提示音",
        CanChooseFiles: true,
        CanChooseDirectories: false,
        AllowsMultipleSelection: false,
        Filters: [{ DisplayName: "WAV 音檔 (*.wav)", Pattern: "*.wav" }],
      });
    } catch (err) {
      if (isDialogCancelled(err)) {
        noteDialogCancelled(err instanceof Error ? err.message : String(err));
        return;
      }
      setLastError(friendlyError(err));
      return;
    }
    // macOS reports cancellation as an empty path, not a rejection.
    if (!path) {
      noteDialogCancelled("empty path");
      return;
    }
    addCustomMutation.mutate(path);
  }

  function saveGamePath(path: string) {
    setGamePathError(undefined);
    setGamePathMutation.mutate(path, { onSuccess: () => setGamePathInput(undefined) });
  }

  // Skips the auto-save when focus is moving to one of this section's own buttons, so their click isn't swallowed by the save disabling them mid-click.
  function handleGamePathBlur(e: FocusEvent<HTMLInputElement>) {
    const trimmed = gamePathValue.trim();
    if (trimmed === savedGamePath) return;
    const next = e.relatedTarget;
    if (next instanceof Node && gamePathSectionRef.current?.contains(next)) return;
    saveGamePath(trimmed);
  }

  function handleRestoreGamePath() {
    setGamePathInput(undefined);
    setGamePathError(undefined);
    testGamePathMutation.reset();
  }

  function handleClearGamePath() {
    testGamePathMutation.reset();
    setGamePathInput("");
    saveGamePath("");
  }

  // Under an env override the input is disabled and may hold a stale setting value, so testing always asks the backend for the effective path instead.
  function handleTestGamePath() {
    testGamePathMutation.mutate(isEnvGamePath ? "" : gamePathValue.trim());
  }

  async function handleBrowseGamePath() {
    setGamePathError(undefined);
    testGamePathMutation.reset();
    let path: string;
    try {
      path = await Dialogs.OpenFile({
        Title: "選擇 MapleStory.exe",
        CanChooseFiles: true,
        CanChooseDirectories: false,
        AllowsMultipleSelection: false,
        Filters: [
          { DisplayName: "MapleStory.exe", Pattern: "MapleStory.exe" },
          { DisplayName: "執行檔 (*.exe)", Pattern: "*.exe" },
        ],
      });
    } catch (err) {
      if (isDialogCancelled(err)) {
        noteDialogCancelled(err instanceof Error ? err.message : String(err));
        return;
      }
      setGamePathError(friendlyError(err));
      return;
    }
    // macOS reports cancellation as an empty path, not a rejection.
    if (!path) {
      noteDialogCancelled("empty path");
      return;
    }
    setGamePathInput(path);
    saveGamePath(path);
  }

  function isChecked(snd: Sound): boolean {
    return !!selected.data && sameSound(snd, selected.data.sound);
  }

  function renderRow(opt: Option) {
    const isNone = opt.sound.kind === "none";
    const isCustom = opt.sound.kind === "custom";
    return (
      <li
        key={`${opt.sound.kind}:${opt.sound.name ?? ""}:${opt.sound.path ?? ""}`}
        className="flex items-center gap-2 px-3 py-2"
      >
        <label className="flex flex-1 items-center gap-2">
          <input
            type="radio"
            name="alert-sound"
            className="size-4"
            checked={isChecked(opt.sound)}
            onChange={() => selectSound(opt.sound)}
          />
          <span className="flex-1 text-sm">{opt.label}</span>
        </label>
        {opt.missing && (
          <span className="flex items-center gap-1 text-xs text-amber-600">
            <TriangleAlert className="size-3.5" />
            找不到檔案
          </span>
        )}
        {!isNone && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`試聽 ${opt.label}`}
            title="試聽"
            disabled={opt.missing}
            onClick={() => previewSound(opt.sound)}
          >
            <Play />
          </Button>
        )}
        {isCustom && opt.sound.path && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`從清單移除 ${opt.label}`}
            title="從清單移除"
            onClick={() => removeCustomSound(opt.sound.path!)}
          >
            <X />
          </Button>
        )}
      </li>
    );
  }

  function renderGroupHeading(text: string) {
    return (
      <li className="px-3 pt-3 pb-1 text-xs font-medium text-muted-foreground">
        {text}
      </li>
    );
  }

  function renderList() {
    if (options.isPending) {
      return <p className="p-3 text-sm text-muted-foreground">載入中…</p>;
    }
    if (options.isError) {
      return (
        <p className="p-3 text-sm text-destructive">
          載入失敗:{friendlyError(options.error)}
        </p>
      );
    }
    const rows: ReactNode[] = [];
    let lastGroup: string | undefined;
    for (const opt of options.data) {
      if (opt.group !== lastGroup) {
        const label = GROUP_LABELS[opt.group];
        if (label) rows.push(renderGroupHeading(label));
        lastGroup = opt.group;
      }
      rows.push(renderRow(opt));
    }
    return (
      <div role="radiogroup" aria-labelledby={GROUP_HEADING_ID}>
        <ul className="flex max-h-72 flex-col divide-y overflow-y-auto">{rows}</ul>
      </div>
    );
  }

  // Under an env override the browse button is disabled, so the placeholder must not tell the user to click it.
  function gamePathPlaceholder(): string {
    if (isEnvGamePath) return "";
    if (gamePath.data?.detected) return gamePath.data.detected;
    return "找不到遊戲，請按瀏覽選擇 MapleStory.exe";
  }

  // envGamePathInvalidMessage is the shared copy for an env-overridden game path that fails checkGameExe, used by both the test result and the effective-path hint.
  function envGamePathInvalidMessage(): string {
    return "環境變數 BEANFUN_GAME_EXE 指定的路徑無效";
  }

  function renderGamePathTestResult() {
    const result = testGamePathMutation.data;
    if (!result) return null;
    if (result.ok) {
      return (
        <div className="flex flex-col gap-0.5 text-xs">
          <p className="text-emerald-600">✓ 找到遊戲：{result.path}</p>
          {result.warnings?.map((w) => (
            <p key={w} className="text-amber-600">
              ⚠ {friendlyGameCheckWarning(w)}
            </p>
          ))}
        </div>
      );
    }
    if (isEnvGamePath) {
      return <p className="text-xs text-destructive">{envGamePathInvalidMessage()}</p>;
    }
    return (
      <p className="text-xs text-destructive">{friendlyGameCheckProblem(result.problem ?? "")}</p>
    );
  }

  // Under an env override the browse button is disabled, so this must not tell the user to click it — the env notice above already names the offending path.
  function renderGamePathEffectiveHint() {
    if (!gamePath.data || gamePath.data.check.ok) return null;
    if (isEnvGamePath) {
      return (
        <p className="flex items-center gap-1 text-xs text-amber-600">
          <TriangleAlert className="size-3.5" />
          {envGamePathInvalidMessage()}
        </p>
      );
    }
    return (
      <p className="flex items-center gap-1 text-xs text-amber-600">
        <TriangleAlert className="size-3.5" />
        {friendlyGameCheckProblem(gamePath.data.check.problem ?? "")}
      </p>
    );
  }

  function renderGamePathEnvNotice() {
    if (!isEnvGamePath) return null;
    return (
      <p className="flex items-center gap-1 text-xs text-amber-600">
        <TriangleAlert className="size-3.5" />
        目前由環境變數 BEANFUN_GAME_EXE 指定：{gamePath.data?.effective}，這裡的設定不會生效
      </p>
    );
  }

  return (
    <AppShell mainClassName="flex-col items-stretch p-0">
      <div className="flex items-center gap-2 border-b p-4">
        <Button variant="ghost" size="sm" onClick={goBack}>
          ← 返回
        </Button>
        <h1 className="text-base font-semibold">設定</h1>
      </div>

      <section ref={gamePathSectionRef} className="flex flex-col gap-2 border-b p-4">
        <div>
          <h2 className="text-sm font-medium">遊戲位置</h2>
          <p className="text-xs text-muted-foreground">
            自訂 MapleStory.exe 的位置，留空則自動偵測
          </p>
        </div>

        <div className="flex items-center gap-2">
          <input
            type="text"
            aria-label="遊戲執行檔路徑"
            className="h-7 flex-1 rounded-md border border-border bg-background px-2 text-sm placeholder:text-muted-foreground dark:border-input dark:bg-input/30"
            value={gamePathValue}
            placeholder={gamePathPlaceholder()}
            disabled={isEnvGamePath}
            onChange={(e) => {
              testGamePathMutation.reset();
              setGamePathInput(e.target.value);
            }}
            onBlur={handleGamePathBlur}
          />
          <Button
            variant="outline"
            size="sm"
            disabled={isEnvGamePath || setGamePathMutation.isPending}
            onClick={handleBrowseGamePath}
          >
            瀏覽…
          </Button>
          {savedGamePath && (
            <Button
              variant="ghost"
              size="sm"
              disabled={isEnvGamePath || setGamePathMutation.isPending}
              onClick={handleClearGamePath}
            >
              清除
            </Button>
          )}
        </div>

        {renderGamePathEnvNotice()}

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={testGamePathMutation.isPending || setGamePathMutation.isPending}
            onClick={handleTestGamePath}
          >
            {testGamePathMutation.isPending ? "測試中…" : "測試"}
          </Button>
          <div aria-live="polite">{renderGamePathTestResult()}</div>
        </div>

        {gamePathError && (
          <div className="flex items-center gap-2">
            <p className="text-xs text-destructive">{gamePathError}</p>
            {gamePathInput !== undefined && (
              <Button variant="ghost" size="sm" onClick={handleRestoreGamePath}>
                還原
              </Button>
            )}
          </div>
        )}
        {renderGamePathEffectiveHint()}
      </section>

      <section className="flex flex-col gap-2 p-4">
        <div>
          <h2 id={GROUP_HEADING_ID} className="text-sm font-medium">
            伺服器開機提示音
          </h2>
          <p className="text-xs text-muted-foreground">
            伺服器從關閉變成開啟時播放
          </p>
        </div>

        <Card className="gap-0 py-0">{renderList()}</Card>

        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleAddSound}>
            <Plus />
            加入音檔…
          </Button>
          <span className="text-xs text-muted-foreground">
            只支援 WAV 檔
          </span>
        </div>

        {lastError && <p className="text-xs text-destructive">{lastError}</p>}
      </section>
    </AppShell>
  );
}
