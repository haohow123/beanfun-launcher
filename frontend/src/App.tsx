import { useAtom, useAtomValue } from "jotai";

import { type LoginInfo } from "@bindings/beanfun";
import { HomePage } from "@/pages/HomePage";
import { LoginPage } from "@/pages/LoginPage";
import { SettingsPage } from "@/pages/SettingsPage";
import { useGameStateEventBridge } from "@/queries/gameState";
import { useLoginsChangedBridge, useLoginsQuery } from "@/queries/logins";
import { useMapleStatusEventBridge } from "@/queries/mapleStatus";
import { useQRStateEventBridge } from "@/queries/qrLogin";
import { useSessionEndedBridge } from "@/queries/session";
import { addingLoginAtom } from "@/state/auth";
import { settingsOpenAtom } from "@/state/settings";

function routeFor(
  logins: LoginInfo[] | undefined,
  adding: boolean,
  closeAdding: () => void,
) {
  if (logins === undefined) return null;
  if (logins.length === 0) return <LoginPage />;
  if (adding) return <LoginPage onBack={closeAdding} />;
  return <HomePage />;
}

function App() {
  // Single subscriber for the backend's game:state-changed push event;
  // funnels payloads into the gameStateQueryKey React Query cache so
  // HomePage's smart button derives label/action reactively. Lives
  // here (not inside HomePage) so the subscription survives logout →
  // login navigation without a re-mount churn.
  useGameStateEventBridge();
  useMapleStatusEventBridge();
  useQRStateEventBridge();
  useSessionEndedBridge();
  useLoginsChangedBridge();

  const logins = useLoginsQuery();
  const [adding, setAdding] = useAtom(addingLoginAtom);
  const settingsOpen = useAtomValue(settingsOpenAtom);

  // A failed list read falls back to the login page rather than a blank screen.
  const list = logins.isError ? [] : logins.data;

  // hidden (not unmount) keeps Login/Home state alive across the round trip
  // to settings — the QR tile / account list shouldn't reset on ← 返回.
  return (
    <>
      <div hidden={settingsOpen}>
        {routeFor(list, adding, () => setAdding(false))}
      </div>
      {settingsOpen && <SettingsPage />}
    </>
  );
}

export default App;
