import { vi } from "vitest";

type AnyFn = (...args: never[]) => unknown;

/** fakeService turns every export of a generated service into a vi.fn resolving to the given default, or undefined. */
export function fakeService<T extends object>(
  original: T,
  defaults: Partial<Record<keyof T, AnyFn>> = {},
): T {
  const fake: Record<string, unknown> = {};
  for (const key of Object.keys(original)) {
    const impl = (defaults as Record<string, AnyFn | undefined>)[key];
    fake[key] = vi.fn(impl ?? (async () => undefined));
  }
  return fake as T;
}
