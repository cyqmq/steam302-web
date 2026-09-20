#!/usr/bin/env bash
# 切回原版 steam302：停新版 → 清空我们写的 S302X 劫持 → 启动原版
# 用法: sudo bash deploy/switch-back.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "[1/3] 停止新版服务..."
systemctl stop steam302-web-caddy.service 2>/dev/null || true
systemctl stop steam302-web-fwd.service  2>/dev/null || true

echo "[2/3] 清除我们写入的 S302X hosts 劫持..."
./bin/genhosts remove || true

echo "[3/3] 启动原版 steam302..."
systemctl enable steam302.service >/dev/null 2>&1 || true
systemctl start steam302.service
echo "完成。校验: curl -skI https://steamcommunity.com/ (200)  ; 注: github 原版只回空 200"