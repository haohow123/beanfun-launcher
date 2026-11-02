import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

import { clearWailsEventHandlers, onWailsEvent } from "./wailsEvents";

vi.mock("@bindings/beanfun", async (importOriginal) => {
  const orig = await importOriginal<typeof import("@bindings/beanfun")>();
  const { fakeService } = await import("./fakeService");
  return {
    ...orig,
    LoginService: fakeService(orig.LoginService, {
      ListLogins: async () => [],
      GetAccounts: async () => [],
      StartQRLogin: async () => new orig.QRStart(),
      QRStatusNow: async () => new orig.QRState(),
    }),
  };
});

vi.mock("@bindings/launcher", async (importOriginal) => {
  const orig = await importOriginal<typeof import("@bindings/launcher")>();
  const { fakeService } = await import("./fakeService");
  return {
    ...orig,
    LauncherService: fakeService(orig.LauncherService, {
      GamePath: async () =>
        new orig.GamePathInfo({ check: new orig.GameCheck({ ok: true }) }),
      GetGameState: async () => new orig.GameState(),
      GetOTP: async () => "000000",
      LaunchAccount: async () => new orig.LaunchResult(),
    }),
  };
});

vi.mock("@bindings/alertsound", async (importOriginal) => {
  const orig = await importOriginal<typeof import("@bindings/alertsound")>();
  const { fakeService } = await import("./fakeService");
  return {
    ...orig,
    Service: fakeService(orig.Service, {
      Options: async () => [],
      Selected: async () => new orig.Selection(),
    }),
  };
});

vi.mock("@bindings/maple", async (importOriginal) => {
  const orig = await importOriginal<typeof import("@bindings/maple")>();
  const { fakeService } = await import("./fakeService");
  return {
    ...orig,
    MapleService: fakeService(orig.MapleService, {
      ServerStatus: async () => new orig.Status({ online: true }),
    }),
  };
});

vi.mock("@bindings/settings", async (importOriginal) => {
  const orig = await importOriginal<typeof import("@bindings/settings")>();
  const { fakeService } = await import("./fakeService");
  return {
    ...orig,
    Service: fakeService(orig.Service, {
      Status: async () => new orig.Status(),
    }),
  };
});

// The real Create helpers stay because the generated models import them at load time.
vi.mock("@wailsio/runtime", async (importOriginal) => {
  const orig = await importOriginal<typeof import("@wailsio/runtime")>();
  orig.setTransport({
    call: async (objectID, method) => {
      const object =
        Object.entries(orig.objectNames).find(([, id]) => id === objectID)?.[0] ??
        String(objectID);
      throw new Error(`Unmocked Wails runtime call: ${object} method ${method}`);
    },
  });
  return {
    ...orig,
    Events: {
      ...orig.Events,
      On: vi.fn((name: string, handler: Parameters<typeof onWailsEvent>[1]) =>
        onWailsEvent(name, handler),
      ),
    },
    Clipboard: { ...orig.Clipboard, SetText: vi.fn(async () => undefined) },
    Dialogs: {
      ...orig.Dialogs,
      OpenFile: vi.fn(async () => ""),
      Info: vi.fn(async () => ""),
      Warning: vi.fn(async () => ""),
      Error: vi.fn(async () => ""),
      Question: vi.fn(async () => ""),
    },
  };
});

beforeEach(() => {
  clearWailsEventHandlers();
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
