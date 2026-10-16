import { useAtom } from "jotai";
import { ChevronDown, ChevronRight, Pencil } from "lucide-react";
import { type KeyboardEvent, type ReactNode, useRef, useState } from "react";

import { type Account, type LoginInfo } from "@bindings/beanfun";
import { Button } from "@/components/ui/button";
import { friendlyError } from "@/lib/errors";
import { useAccountsQuery } from "@/queries/accounts";
import { MAX_LOGIN_NAME_LENGTH, useRenameLoginMutation } from "@/queries/logins";
import { collapsedLoginsAtom } from "@/state/auth";

interface LoginGroupProps {
  login: LoginInfo;
  logoutPending: boolean;
  onLogout: (loginId: string) => void;
  renderCard: (loginId: string, acc: Account) => ReactNode;
}

export function LoginGroup({
  login,
  logoutPending,
  onLogout,
  renderCard,
}: LoginGroupProps) {
  const accounts = useAccountsQuery(login.id);
  const [collapsedMap, setCollapsedMap] = useAtom(collapsedLoginsAtom);

  const expanded = !collapsedMap[login.id];
  const rename = useRenameLoginMutation();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [renameError, setRenameError] = useState<string | null>(null);
  // Enter and Escape unmount the input, and the blur that follows must not save a second time.
  const closingRef = useRef(false);

  function toggle() {
    setCollapsedMap((m) => ({ ...m, [login.id]: expanded }));
  }

  function startEditing() {
    closingRef.current = false;
    setDraft(login.name);
    setRenameError(null);
    setEditing(true);
  }

  function cancelEditing() {
    closingRef.current = true;
    setRenameError(null);
    setEditing(false);
  }

  function saveName() {
    if (closingRef.current) return;
    const name = draft.trim();
    if (name === login.name) {
      cancelEditing();
      return;
    }
    closingRef.current = true;
    rename.mutate(
      { loginId: login.id, name },
      {
        onSuccess: () => setEditing(false),
        onError: (err) => {
          closingRef.current = false;
          setRenameError(friendlyError(err));
        },
      },
    );
  }

  function handleNameKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    // WebKit reports the Enter that confirms an IME candidate as keyCode 229 with isComposing false.
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    if (e.key === "Enter") {
      e.preventDefault();
      saveName();
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      cancelEditing();
    }
  }

  function renderName() {
    if (editing) {
      return (
        <input
          type="text"
          aria-label="群組名稱"
          placeholder="留空恢復預設名稱"
          autoFocus
          maxLength={MAX_LOGIN_NAME_LENGTH}
          value={draft}
          readOnly={rename.isPending}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={handleNameKeyDown}
          onBlur={saveName}
          className="h-7 min-w-0 flex-1 rounded-md border border-border bg-background px-2 text-sm placeholder:text-muted-foreground dark:border-input dark:bg-input/30"
        />
      );
    }
    return (
      <>
        <span className="min-w-0 truncate text-sm font-semibold">
          {login.label}
        </span>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={`重新命名 ${login.label}`}
          onClick={startEditing}
        >
          <Pencil />
        </Button>
      </>
    );
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
        {renderName()}
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
      {renameError && (
        <p className="text-xs text-destructive">{renameError}</p>
      )}
      {expanded && renderBody()}
    </div>
  );
}
