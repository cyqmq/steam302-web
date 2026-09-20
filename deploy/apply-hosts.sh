#!/usr/bin/env bash
# 切到新版：把我们的 S302X 劫持写入 /etc/hosts（保留原文件备份）
# 用法: sudo bash deploy/apply-hosts.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ ! -f S302.hosts ]]; then
  ./bin/genconfig --hosts "$ROOT/S302.hosts" >/dev/null
fi
./bin/genhosts apply S302.hosts
echo "已应用 S302.hosts 到 /etc/hosts（$(grep -c '#S302X' /etc/hosts || true) 行 S302X）"
echo "校验: curl -sk https://github.com/ && curl -sk https://steamcommunity.com/"