import { useAtomValue } from "jotai";

import { HomePage } from "@/pages/HomePage";
import { LoginPage } from "@/pages/LoginPage";
import { useGameStateEventBridge } from "@/queries/gameState";
import { useMapleStatusEventBridge } from "@/queries/mapleStatus";
import { loggedInAtom } from "@/state/auth";

function App() {
  // Single subscriber for the backend's game:state-changed push event;
  // funnels payloads into the gameStateQueryKey React Query cache so
  // HomePage's smart button derives label/action reactively. Lives
  // here (not inside HomePage) so the subscription survives logout →
  // login navigation without a re-mount churn.
  useGameStateEventBridge();
  useMapleStatusEventBridge();

  const loggedIn = useAtomValue(loggedInAtom);
  return loggedIn ? <HomePage /> : <LoginPage />;
}

export default App;
