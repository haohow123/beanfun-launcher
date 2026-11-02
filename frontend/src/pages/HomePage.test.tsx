import { screen, waitFor } from "@testing-library/react";
import { type CancellablePromise, Clipboard } from "@wailsio/runtime";
import { describe, expect, it, vi } from "vitest";

import { Account, LoginInfo, LoginService } from "@bindings/beanfun";
import App from "@/App";
import { accountsQueryKey } from "@/queries/accounts";
import { renderWithProviders } from "@/test/render";
import { emitWailsEvent } from "@/test/wailsEvents";

const SESSION_ENDED_EVENT = "beanfun:session-ended";

const loginA = new LoginInfo({ id: "a", label: "帳號甲", name: "" });
const loginB = new LoginInfo({ id: "b", label: "帳號乙", name: "" });

const accountsByLogin: Record<string, Account[]> = {
  a: [new Account({ sid: "sid-a", ssn: "ssn-a", sname: "楓之谷甲" })],
  b: [new Account({ sid: "sid-b", ssn: "ssn-b", sname: "楓之谷乙" })],
};

function seedLogins(logins: LoginInfo[]) {
  vi.mocked(LoginService.ListLogins).mockResolvedValue(logins);
  vi.mocked(LoginService.GetAccounts).mockImplementation(
    (id: string) =>
      Promise.resolve(accountsByLogin[id] ?? []) as CancellablePromise<Account[]>,
  );
}

async function renderHome(logins: LoginInfo[]) {
  seedLogins(logins);
  const view = renderWithProviders(<App />);
  await screen.findByRole("button", { name: `收合 ${logins[0].label}` });
  return view;
}

describe("HomePage login groups", () => {
  it("starts every group expanded and collapses one at a time", async () => {
    const { user } = await renderHome([loginA, loginB]);

    expect(await screen.findByText("sid-a")).toBeVisible();
    expect(await screen.findByText("sid-b")).toBeVisible();

    await user.click(screen.getByRole("button", { name: "收合 帳號甲" }));

    expect(screen.queryByText("sid-a")).toBeNull();
    expect(screen.getByText("sid-b")).toBeVisible();
    expect(screen.getByRole("button", { name: "展開 帳號甲" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(screen.getByRole("button", { name: "收合 帳號乙" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
  });

  it("removes only the login that ended and shows the notice", async () => {
    const { queryClient } = await renderHome([loginA, loginB]);
    await screen.findByText("sid-b");
    expect(queryClient.getQueryData(accountsQueryKey("a"))).toBeDefined();
    expect(queryClient.getQueryData(accountsQueryKey("b"))).toBeDefined();
    const accountCallsForA = vi
      .mocked(LoginService.GetAccounts)
      .mock.calls.filter(([id]) => id === "a").length;

    seedLogins([loginA]);
    await emitWailsEvent(SESSION_ENDED_EVENT, {
      loginId: "b",
      label: "帳號乙",
    });

    expect(await screen.findByText("帳號乙 的登入已失效")).toBeVisible();
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "收合 帳號乙" })).toBeNull();
    });
    expect(screen.getByRole("button", { name: "收合 帳號甲" })).toBeVisible();
    expect(screen.getByText("sid-a")).toBeVisible();
    expect(queryClient.getQueryData(accountsQueryKey("b"))).toBeUndefined();
    expect(queryClient.getQueryData(accountsQueryKey("a"))).toBeDefined();
    const accountCallsForAAfter = vi
      .mocked(LoginService.GetAccounts)
      .mock.calls.filter(([id]) => id === "a").length;
    expect(accountCallsForAAfter).toBe(accountCallsForA);
  });

  it("returns to the login page when the last login ends", async () => {
    await renderHome([loginA]);
    await screen.findByText("sid-a");

    seedLogins([]);
    await emitWailsEvent(SESSION_ENDED_EVENT, {
      loginId: "a",
      label: "帳號甲",
    });

    expect(
      await screen.findByRole("heading", { name: "登入 Beanfun" }),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "收合 帳號甲" })).toBeNull();
  });
});

describe("HomePage adding a login", () => {
  it("cancels the QR login when going back from the add page", async () => {
    const { user } = await renderHome([loginA]);

    await user.click(
      screen.getByRole("button", { name: "＋ 新增 Beanfun 帳號" }),
    );
    const back = await screen.findByRole("button", { name: "← 返回" });
    expect(LoginService.CancelQRLogin).not.toHaveBeenCalled();

    await user.click(back);

    await waitFor(() => {
      expect(LoginService.CancelQRLogin).toHaveBeenCalledTimes(1);
    });
    expect(
      await screen.findByRole("button", { name: "收合 帳號甲" }),
    ).toBeVisible();
  });
});

describe("HomePage copy credentials", () => {
  it("copies the account and OTP through the Wails clipboard", async () => {
    const { user } = await renderHome([loginA]);
    const { LauncherService } = await import("@bindings/launcher");
    vi.mocked(LauncherService.GetOTP).mockResolvedValue("123456");

    await user.click(await screen.findByRole("button", { name: "複製帳密" }));

    expect(await screen.findByText("✓ 已複製")).toBeVisible();
    expect(Clipboard.SetText).toHaveBeenCalledWith("sid-a\n123456");
  });

  it("shows 複製失敗 when the clipboard write is rejected", async () => {
    const { user } = await renderHome([loginA]);
    const { LauncherService } = await import("@bindings/launcher");
    vi.mocked(LauncherService.GetOTP).mockResolvedValue("123456");
    vi.mocked(Clipboard.SetText).mockRejectedValue(new Error("denied"));
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);

    await user.click(await screen.findByRole("button", { name: "複製帳密" }));

    expect(await screen.findByRole("button", { name: "複製失敗" })).toBeVisible();
    expect(Clipboard.SetText).toHaveBeenCalledTimes(1);
    expect(consoleError).toHaveBeenCalledWith(
      "clipboard write failed:",
      expect.objectContaining({ message: "denied" }),
    );
  });
});
