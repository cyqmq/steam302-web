#!/usr/bin/env bash
# steam302-web systemd 部署
# 用法: sudo bash deploy/install.sh   （安装 3 个单元，不自动启动）
# 切换新版:  sudo systemctl start steam302-web-caddy steam302-web-fwd
#            （Conflicts 会自动停掉原版 steam302.service）
# 切回原版:  sudo bash deploy/switch-back.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UNIT_DIR=/etc/systemd/system
CADDY_BIN="$(command -v caddy || true)"

if [[ -z "$CADDY_BIN" ]]; then
  echo "错误: 未找到 caddy，请先安装并放入 PATH" >&2
  exit 1
fi

echo "项目根目录: $ROOT"
echo "caddy:      $CADDY_BIN"

# 确保二进制齐全（缺则构建）
export PATH="$PATH:/usr/local/go/bin"
for b in genconfig genhosts genpki s302fwd webui; do
  if [[ ! -x "$ROOT/bin/$b" ]]; then
    echo "缺少 bin/$b，正在构建..."
    (cd "$ROOT" && go build -o "bin/$b" "./cmd/$b")
  fi
done

# 基准配置（保证幂等安装）
(cd "$ROOT" && ./bin/genconfig --hosts "$ROOT/S302.hosts" >/dev/null)

cat > "$UNIT_DIR/steam302-web-caddy.service" <<EOF
[Unit]
Description=steam302-web Caddy MITM reverse proxy
After=network-online.target
Wants=network-online.target
# 与本机原版 steam302 互斥：启动本单元会自动停止原版（避免争抢 443/80）
Conflicts=steam302.service

[Service]
Type=simple
User=root
WorkingDirectory=$ROOT
ExecStartPre=$ROOT/bin/genconfig --hosts $ROOT/S302.hosts
ExecStart=$CADDY_BIN run --config $ROOT/Caddyfile --adapter caddyfile
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

cat > "$UNIT_DIR/steam302-web-fwd.service" <<EOF
[Unit]
Description=steam302-web port forward 80/443 -> 24196/25584
After=network-online.target steam302-web-caddy.service
Wants=network-online.target
Conflicts=steam302.service

[Service]
Type=simple
User=root
WorkingDirectory=$ROOT
ExecStart=$ROOT/bin/s302fwd run
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

cat > "$UNIT_DIR/steam302-web-webui.service" <<EOF
[Unit]
Description=steam302-web management console
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=$ROOT
ExecStart=$ROOT/bin/webui --addr 127.0.0.1:34902
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
echo
echo "已安装:"
echo "  steam302-web-caddy.service  (MITM 反代, 监听 24196/25584)"
echo "  steam302-web-fwd.service    (80/443 转发到 24196/25584)"
echo "  steam302-web-webui.service  (管理台 http://127.0.0.1:34902)"
echo
echo "使用:"
echo "  切到新版(自动停原版):  sudo systemctl start steam302-web-caddy steam302-web-fwd && sudo bash deploy/apply-hosts.sh"
echo "  设开机自启(只启新家族): sudo systemctl enable steam302-web-caddy steam302-web-fwd steam302-web-webui"
echo "  切回原版:               sudo bash deploy/switch-back.sh"
echo
echo "注意: 原版与新版的 systemd 单元互相 Conflicts，两边不能同时 enable。"