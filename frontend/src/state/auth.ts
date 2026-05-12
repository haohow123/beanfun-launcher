import { atom } from "jotai";

/**
 * loggedInAtom is the app-wide auth gate. Pages flip it via
 * useSetAtom; App.tsx reads it via useAtomValue to pick the route.
 *
 * Today this is a bare boolean — once we have real Session info to
 * expose (account name, web token expiry, etc.), this should become
 * an atom of `Session | null` and pull a SessionInfo DTO from the
 * Go LoginService. See docs/beanfun-login-protocol.md § Token storage.
 */
export const loggedInAtom = atom(false);
