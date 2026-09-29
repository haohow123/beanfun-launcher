import { useAtomValue } from "jotai";

import { HomePage } from "@/pages/HomePage";
import { LoginPage } from "@/pages/LoginPage";
import { SettingsPage } from "@/pages/SettingsPage";
import { useGameStateEventBridge } from "@/queries/gameState";
import { useMapleStatusEventBridge } from "@/queries/mapleStatus";
import { useQRStateEventBridge } from "@/queries/qrLogin";
import { loggedInAtom } from "@/state/auth";
import { settingsOpenAtom } from "@/state/settings";

function App() {
  // Single subscriber for the backend's game:state-changed push event;
  // funnels payloads into the gameStateQueryKey React Query cache so
  // HomePage's smart button derives label/action reactively. Lives
  // here (not inside HomePage) so the subscription survives logout →
  // login navigation without a re-mount churn.
  useGameStateEventBridge();
  useMapleStatusEventBridge();
  useQRStateEventBridge();

  const loggedIn = useAtomValue(loggedInAtom);
  const settingsOpen = useAtomValue(settingsOpenAtom);

  // hidden (not unmount) keeps Login/Home state alive across the round trip
  // to settings — the QR tile / account list shouldn't reset on ← 返回.
  return (
    <>
      <div hidden={settingsOpen}>{loggedIn ? <HomePage /> : <LoginPage />}</div>
      {settingsOpen && <SettingsPage />}
    </>
  );
}

export default App;
