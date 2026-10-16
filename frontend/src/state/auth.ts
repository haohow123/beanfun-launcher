import { atom } from "jotai";

/** sessionEndNoticeAtom is set when a login ended without the user logging it out. */
export const sessionEndNoticeAtom = atom<{ label: string } | null>(null);

/** addingLoginAtom is true while the user is scanning a QR to add another login. */
export const addingLoginAtom = atom(false);

/** collapsedLoginsAtom holds each login's manual collapse choice, keyed by login id. */
export const collapsedLoginsAtom = atom<Record<string, boolean>>({});
