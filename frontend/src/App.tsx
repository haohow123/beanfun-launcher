import { useState } from "react";

import { HomePage } from "@/pages/HomePage";
import { LoginPage } from "@/pages/LoginPage";

function App() {
  const [loggedIn, setLoggedIn] = useState(false);

  if (loggedIn) {
    return <HomePage onLogout={() => setLoggedIn(false)} />;
  }
  return <LoginPage onApproved={() => setLoggedIn(true)} />;
}

export default App;
