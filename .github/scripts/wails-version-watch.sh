#!/usr/bin/env bash
# DRY_RUN=1 prints the gh write commands instead of running them.
set -euo pipefail

title_prefix="升級 Wails 到"

current=$(awk '$1 == "github.com/wailsapp/wails/v3" { print $2 }' go.mod)
go_latest=$(curl -fsS https://proxy.golang.org/github.com/wailsapp/wails/v3/@latest | jq -r .Version)
npm_latest=$(curl -fsS https://registry.npmjs.org/@wailsio/runtime/latest | jq -r .version)
npm_locked=$(jq -r '.packages["node_modules/@wailsio/runtime"].version' frontend/package-lock.json)

echo "go.mod: $current | Go latest: $go_latest | npm latest: $npm_latest | npm locked: $npm_locked"

if [ -z "$current" ] || [ -z "$go_latest" ] || [ "$go_latest" = "null" ]; then
  echo "could not read versions" >&2
  exit 1
fi

if [ "$current" = "$go_latest" ]; then
  echo "Wails is up to date"
  exit 0
fi

npm_note="npm 的 \`@wailsio/runtime\` 最新版也是 \`$npm_latest\`，兩邊版號一致。"
if [ "${go_latest#v}" != "$npm_latest" ]; then
  npm_note="**注意**：npm 的 \`@wailsio/runtime\` 最新版是 \`$npm_latest\`，與 Go 端的 \`$go_latest\` 不一致。"
fi

run() {
  if [ "${DRY_RUN:-}" = "1" ]; then
    printf 'DRY_RUN: %s\n' "$*"
    return
  fi
  "$@"
}

existing=$(gh issue list --state open --search "in:title \"$title_prefix\"" --json number,title \
  --jq "[.[] | select(.title | startswith(\"$title_prefix\"))][0] // empty | \"\(.number)\t\(.title)\"")

if [ -n "$existing" ]; then
  number=${existing%%$'\t'*}
  title=${existing#*$'\t'}
  if [[ "$title" == *"${go_latest#v}"* ]]; then
    echo "#$number already targets $go_latest"
    exit 0
  fi
  run gh issue comment "$number" --body "Wails 又出新版了：\`$go_latest\`（\`go.mod\` 目前是 \`$current\`）。$npm_note 升級時請以最新版為目標。"
  exit 0
fi

body=$(cat <<EOF
## 現況

- \`go.mod\` 的 Wails：\`$current\`
- Go proxy 上的最新版：\`$go_latest\`
- 前端 runtime：lockfile 目前是 \`$npm_locked\`，npm 最新版是 \`$npm_latest\`

$npm_note

## 待確認

- $current → $go_latest 之間的破壞性變更，以及各自影響哪些檔案
- \`wails3\` CLI（本機與 \`.github/workflows/release.yml\`）要一起升
- 升級後的驗證範圍：QR 登入、啟動遊戲、帳密注入、伺服器狀態與提示音、設定頁選檔，以及 Windows 打包

這張票由 \`.github/workflows/wails-version-watch.yml\` 自動開出。
EOF
)

run gh issue create --title "$title_prefix ${go_latest}" --label dependencies --body "$body"
