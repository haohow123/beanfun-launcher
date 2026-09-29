import { Dialogs } from "@wailsio/runtime";
import { useSetAtom } from "jotai";
import { Play, Plus, TriangleAlert, X } from "lucide-react";
import { useState } from "react";

import { type Option, type Sound } from "@bindings/alertsound";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { friendlyError, isDialogCancelled } from "@/lib/errors";
import {
  sameSound,
  useAddCustomAlertSoundMutation,
  useAlertSoundOptionsQuery,
  useAlertSoundSelectedQuery,
  usePreviewAlertSoundMutation,
  useRemoveCustomAlertSoundMutation,
  useSelectAlertSoundMutation,
} from "@/queries/alertSound";
import { settingsOpenAtom } from "@/state/settings";

function builtinOptions(options: Option[]): Option[] {
  return options.filter((o) => o.sound.kind === "builtin");
}

function customOptions(options: Option[]): Option[] {
  return options.filter((o) => o.sound.kind === "custom");
}

function noneOption(options: Option[]): Option | undefined {
  return options.find((o) => o.sound.kind === "none");
}

// GROUP_HEADING_ID links the radiogroup wrapper to the visible h2 heading it names.
const GROUP_HEADING_ID = "alert-sound-group-heading";

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

  function goBack() {
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
      if (!isDialogCancelled(err)) setLastError(friendlyError(err));
      return;
    }
    if (!path) return; // macOS reports cancellation as an empty path, not a rejection.
    addCustomMutation.mutate(path);
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
    const none = noneOption(options.data);
    const builtins = builtinOptions(options.data);
    const customs = customOptions(options.data);
    return (
      <div role="radiogroup" aria-labelledby={GROUP_HEADING_ID}>
        <ul className="flex max-h-72 flex-col divide-y overflow-y-auto">
          {none && renderRow(none)}
          {renderGroupHeading("Windows 內建")}
          {builtins.map(renderRow)}
          {customs.length > 0 && renderGroupHeading("我的音檔")}
          {customs.map(renderRow)}
        </ul>
      </div>
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
