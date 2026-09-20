#!/usr/bin/env bash
# 重置所有设置 / 卸载：删除本机配置文件与开机自启，恢复默认状态（切回原版 steam302）
# 等价于原版菜单「重置所有设置」。
# 用法: sudo bash deploy/uninstall.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "[1/4] 停止并禁用新版 systemd 单元..."
systemctl stop steam302-web-caddy.service 2>/dev/null || true
systemctl stop steam302-web-fwd.service  2>/dev/null || true
systemctl stop steam302-web-webui.service 2>/dev/null || true
systemctl disable steam302-web-caddy.service steam302-web-fwd.service steam302-web-webui.service 2>/dev/null || true
rm -f /etc/systemd/system/steam302-web-caddy.service
rm -f /etc/systemd/system/steam302-web-fwd.service
rm -f /etc/systemd/system/steam302-web-webui.service
systemctl daemon-reload

echo "[2/4] 清除写入 /etc/hosts 的 #S302X 劫持..."
./bin/genhosts remove 2>/dev/null || true

echo "[3/4] 删除运行时配置（overrides / fwd 日志 / pid）..."
rm -f config/overrides.json
rm -f config/s302fwd.log config/s302fwd.pid
rm -f "$ROOT/Caddyfile" "$ROOT/S302.hosts"

echo "[4/4] 恢复原版 steam302（存在则启动 + 设为默认）..."
if [[ -f /etc/systemd/system/steam302.service ]]; then
  systemctl enable steam302.service >/dev/null 2>&1 || true
  systemctl start steam302.service
fi

echo "完成：已恢复默认状态。"