import { useSetAtom } from "jotai";
import { Play } from "lucide-react";

import { type Option, type Sound } from "@bindings/alertsound";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { friendlyError } from "@/lib/errors";
import {
  sameSound,
  useAlertSoundOptionsQuery,
  useAlertSoundSelectedQuery,
  usePreviewAlertSoundMutation,
  useSelectAlertSoundMutation,
} from "@/queries/alertSound";
import { settingsOpenAtom } from "@/state/settings";

// windowsBuiltinOptions groups the Windows 預設 alias with the *.wav catalogue
// under one "Windows 內建" heading; noneOption and customOptions are the
// other two groups in the list.
function windowsBuiltinOptions(options: Option[]): Option[] {
  return options.filter(
    (o) => o.sound.kind === "default" || o.sound.kind === "builtin",
  );
}

function customOptions(options: Option[]): Option[] {
  return options.filter((o) => o.sound.kind === "custom");
}

function noneOption(options: Option[]): Option | undefined {
  return options.find((o) => o.sound.kind === "none");
}

export function SettingsPage() {
  const setSettingsOpen = useSetAtom(settingsOpenAtom);
  const options = useAlertSoundOptionsQuery();
  const selected = useAlertSoundSelectedQuery();
  const selectMutation = useSelectAlertSoundMutation();
  const previewMutation = usePreviewAlertSoundMutation();

  function goBack() {
    setSettingsOpen(false);
  }

  function selectSound(snd: Sound) {
    selectMutation.mutate(snd);
  }

  function previewSound(snd: Sound) {
    previewMutation.mutate(snd);
  }

  function isChecked(snd: Sound): boolean {
    return !!selected.data && sameSound(snd, selected.data.sound);
  }

  function renderRow(opt: Option) {
    const isNone = opt.sound.kind === "none";
    return (
      <li
        key={`${opt.sound.kind}:${opt.sound.name ?? ""}:${opt.sound.path ?? ""}`}
        className="flex items-center gap-2 px-3 py-2"
      >
        <input
          type="radio"
          name="alert-sound"
          className="size-4"
          checked={isChecked(opt.sound)}
          onChange={() => selectSound(opt.sound)}
        />
        <span className="flex-1 text-sm">{opt.label}</span>
        {!isNone && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="試聽"
            title="試聽"
            onClick={() => previewSound(opt.sound)}
          >
            <Play />
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
    const builtins = windowsBuiltinOptions(options.data);
    const customs = customOptions(options.data);
    return (
      <ul className="flex max-h-72 flex-col divide-y overflow-y-auto">
        {none && renderRow(none)}
        {renderGroupHeading("Windows 內建")}
        {builtins.map(renderRow)}
        {customs.length > 0 && renderGroupHeading("我的音檔")}
        {customs.map(renderRow)}
      </ul>
    );
  }

  function mutationErrorText(): string | undefined {
    const err = selectMutation.error ?? previewMutation.error;
    return err ? friendlyError(err) : undefined;
  }

  const errorText = mutationErrorText();

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
          <h2 className="text-sm font-medium">伺服器開機提示音</h2>
          <p className="text-xs text-muted-foreground">
            伺服器從關閉變成開啟時播放
          </p>
        </div>

        <Card className="gap-0 py-0">{renderList()}</Card>

        {errorText && <p className="text-xs text-destructive">{errorText}</p>}
      </section>
    </AppShell>
  );
}
