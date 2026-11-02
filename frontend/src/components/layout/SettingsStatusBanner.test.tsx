import { screen, waitFor, within, act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Account, LoginInfo, LoginService } from "@bindings/beanfun";
import { Service as AlertSoundService } from "@bindings/alertsound";
import { LauncherService } from "@bindings/launcher";
import { Service as SettingsService, Status } from "@bindings/settings";
import App from "@/App";
import { settingsStatusQueryKey } from "@/queries/settings";
import { renderWithProviders } from "@/test/render";

function seedStatus(problem: string, backupPath = "") {
  vi.mocked(SettingsService.Status).mockResolvedValue(
    new Status({ problem, backupPath }),
  );
}

function seedOneLogin() {
  vi.mocked(LoginService.ListLogins).mockResolvedValue([
    new LoginInfo({ id: "a", label: "帳號甲", name: "" }),
  ]);
  vi.mocked(LoginService.GetAccounts).mockResolvedValue([
    new Account({ sid: "sid-a", ssn: "ssn-a", sname: "楓之谷甲" }),
  ]);
}

describe("SettingsStatusBanner text", () => {
  it("explains an unreadable settings file and offers a reload", async () => {
    seedStatus("unreadable");
    renderWithProviders(<App />);

    const banner = await screen.findByRole("status");
    expect(banner).toHaveTextContent("設定檔目前讀不到");
    expect(banner).toHaveTextContent("恢復前不會存檔");
    expect(
      within(banner).getByRole("button", { name: "重新讀取" }),
    ).toBeVisible();
  });

  it("explains a corrupt settings file without a reload button", async () => {
    seedStatus("corrupt");
    renderWithProviders(<App />);

    const banner = await screen.findByRole("status");
    expect(banner).toHaveTextContent("設定檔內容損壞");
    expect(banner).toHaveTextContent("下次存檔時會先把壞檔備份再重建");
    expect(within(banner).queryByRole("button", { name: "重新讀取" })).toBeNull();
  });

  it("reports where the corrupt file was backed up", async () => {
    seedStatus("", "/data/settings.json.bak");
    renderWithProviders(<App />);

    const banner = await screen.findByRole("status");
    expect(banner).toHaveTextContent(
      "損壞的設定檔已備份到 /data/settings.json.bak",
    );
    expect(
      within(banner).getByRole("button", { name: "關閉通知" }),
    ).toBeVisible();
  });
});

describe("SettingsStatusBanner reload", () => {
  it("re-reads settings, sounds, game path and logins but not the QR login", async () => {
    seedStatus("unreadable");
    const { user } = renderWithProviders(<App />);

    await waitFor(() => {
      expect(LoginService.StartQRLogin).toHaveBeenCalledTimes(1);
    });
    await user.click(await screen.findByRole("button", { name: /^設定/ }));
    const banner = await screen.findByRole("status");
    await screen.findByRole("heading", { name: "設定" });
    await waitFor(() => {
      expect(AlertSoundService.Options).toHaveBeenCalled();
      expect(LauncherService.GamePath).toHaveBeenCalled();
    });

    const before = {
      status: vi.mocked(SettingsService.Status).mock.calls.length,
      options: vi.mocked(AlertSoundService.Options).mock.calls.length,
      selected: vi.mocked(AlertSoundService.Selected).mock.calls.length,
      gamePath: vi.mocked(LauncherService.GamePath).mock.calls.length,
      logins: vi.mocked(LoginService.ListLogins).mock.calls.length,
    };

    await user.click(within(banner).getByRole("button", { name: "重新讀取" }));

    await waitFor(() => {
      expect(SettingsService.Status).toHaveBeenCalledTimes(before.status + 1);
      expect(AlertSoundService.Options).toHaveBeenCalledTimes(before.options + 1);
      expect(AlertSoundService.Selected).toHaveBeenCalledTimes(
        before.selected + 1,
      );
      expect(LauncherService.GamePath).toHaveBeenCalledTimes(before.gamePath + 1);
      expect(LoginService.ListLogins).toHaveBeenCalledTimes(before.logins + 1);
    });
    expect(LoginService.StartQRLogin).toHaveBeenCalledTimes(1);
  });

  it("does not re-fetch the account list of a logged-in group", async () => {
    seedStatus("unreadable");
    seedOneLogin();
    const { user } = renderWithProviders(<App />);

    await screen.findByText("sid-a");
    await waitFor(() => {
      expect(SettingsService.Status).toHaveBeenCalled();
    });
    const accountCalls = vi.mocked(LoginService.GetAccounts).mock.calls.length;
    const loginCalls = vi.mocked(LoginService.ListLogins).mock.calls.length;
    const statusCalls = vi.mocked(SettingsService.Status).mock.calls.length;

    await user.click(await screen.findByRole("button", { name: "重新讀取" }));

    await waitFor(() => {
      expect(LoginService.ListLogins).toHaveBeenCalledTimes(loginCalls + 1);
      expect(SettingsService.Status).toHaveBeenCalledTimes(statusCalls + 1);
    });
    expect(LoginService.GetAccounts).toHaveBeenCalledTimes(accountCalls);
  });
});

describe("SettingsStatusBanner dismissal", () => {
  it("stays hidden across pages until a different backup path appears", async () => {
    seedStatus("", "/data/first.bak");
    seedOneLogin();
    const { user, queryClient } = renderWithProviders(<App />);

    const banner = await screen.findByRole("status");
    await user.click(within(banner).getByRole("button", { name: "關閉通知" }));
    expect(screen.queryByRole("status")).toBeNull();

    await user.click(screen.getByRole("button", { name: /^設定/ }));
    expect(await screen.findByRole("heading", { name: "設定" })).toBeVisible();
    expect(screen.queryByRole("status")).toBeNull();

    seedStatus("", "/data/second.bak");
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: settingsStatusQueryKey });
    });

    expect(await screen.findByRole("status")).toHaveTextContent(
      "/data/second.bak",
    );
  });
});
