import { atom } from "jotai";

/** settingsOpenAtom toggles the SettingsPage overlay in App.tsx. */
export const settingsOpenAtom = atom(false);

/** dismissedBackupPathAtom holds the backup path whose notice the user closed, so the banner stays hidden across pages. */
export const dismissedBackupPathAtom = atom("");
