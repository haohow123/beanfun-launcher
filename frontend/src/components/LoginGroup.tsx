import { useAtom } from "jotai";
import { ChevronDown, ChevronRight } from "lucide-react";
import { type ReactNode } from "react";

import { type Account, type LoginInfo } from "@bindings/beanfun";
import { Button } from "@/components/ui/button";
import { friendlyError } from "@/lib/errors";
import { useAccountsQuery } from "@/queries/accounts";
import { collapsedLoginsAtom } from "@/state/auth";

interface LoginGroupProps {
  login: LoginInfo;
  isFirst: boolean;
  lastUsedSid: string;
  logoutPending: boolean;
  onLogout: (loginId: string) => void;
  renderCard: (loginId: string, acc: Account) => ReactNode;
}

export function LoginGroup({
  login,
  isFirst,
  lastUsedSid,
  logoutPending,
  onLogout,
  renderCard,
}: LoginGroupProps) {
  const accounts = useAccountsQuery(login.id);
  const [collapsedMap, setCollapsedMap] = useAtom(collapsedLoginsAtom);

  const holdsLastUsed =
    lastUsedSid !== "" &&
    (accounts.data?.some((a) => a.sid === lastUsedSid) ?? false);
  const defaultExpanded = isFirst || holdsLastUsed;
  const manual = collapsedMap[login.id];
  const expanded = manual === undefined ? defaultExpanded : !manual;

  function toggle() {
    setCollapsedMap((m) => ({ ...m, [login.id]: expanded }));
  }

  function retry() {
    accounts.refetch();
  }

  function renderBody() {
    if (accounts.isPending) {
      return <p className="text-sm text-muted-foreground">載入帳號中…</p>;
    }
    if (accounts.isError) {
      return (
        <div className="flex flex-col items-center gap-2">
          <p className="text-sm text-destructive">
            載入失敗:{friendlyError(accounts.error)}
          </p>
          <Button variant="outline" onClick={retry}>
            重試
          </Button>
        </div>
      );
    }
    if (accounts.data.length === 0) {
      return (
        <div className="flex flex-col items-center gap-2">
          <p className="text-sm text-muted-foreground">找不到遊戲帳號</p>
          <Button variant="outline" onClick={retry}>
            重試
          </Button>
        </div>
      );
    }
    return (
      <ul className="flex flex-col gap-3">
        {accounts.data.map((acc) => renderCard(login.id, acc))}
      </ul>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-expanded={expanded}
          aria-label={expanded ? `收合 ${login.label}` : `展開 ${login.label}`}
          onClick={toggle}
        >
          {expanded ? <ChevronDown /> : <ChevronRight />}
        </Button>
        <span className="min-w-0 truncate text-sm font-semibold">
          {login.label}
        </span>
        {accounts.data && (
          <span className="shrink-0 text-xs text-muted-foreground">
            {accounts.data.length} 個遊戲帳號
          </span>
        )}
        <Button
          variant="outline"
          size="sm"
          className="ml-auto"
          disabled={logoutPending}
          onClick={() => onLogout(login.id)}
        >
          登出
        </Button>
      </div>
      {expanded && renderBody()}
    </div>
  );
}
