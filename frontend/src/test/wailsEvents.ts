import { act } from "@testing-library/react";

type Handler = (ev: { name: string; data: unknown }) => void;

const handlers = new Map<string, Set<Handler>>();

export function onWailsEvent(name: string, handler: Handler): () => void {
  const set = handlers.get(name) ?? new Set<Handler>();
  set.add(handler);
  handlers.set(name, set);
  return () => {
    set.delete(handler);
  };
}

export function clearWailsEventHandlers() {
  handlers.clear();
}

/** emitWailsEvent delivers a { name, data } event to every subscriber, like the real runtime's WailsEvent. */
export async function emitWailsEvent(name: string, data: unknown = null) {
  await act(async () => {
    for (const handler of [...(handlers.get(name) ?? [])]) {
      handler({ name, data });
    }
  });
}
