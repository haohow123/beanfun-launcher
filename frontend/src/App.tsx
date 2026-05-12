import { useAtomValue } from "jotai";

import { HomePage } from "@/pages/HomePage";
import { LoginPage } from "@/pages/LoginPage";
import { loggedInAtom } from "@/state/auth";

function App() {
  const loggedIn = useAtomValue(loggedInAtom);
  return loggedIn ? <HomePage /> : <LoginPage />;
}

export default App;
