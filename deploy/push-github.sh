#!/usr/bin/env bash
# 自动补推 GitHub：只要任一可用通道出现（SSH ssh.github.com:443 或本机 HTTPS 加速器），
# 就把本地未推送的 main 提交同步到远端，全部推完即退出。
# 用法：deploy/push-github.sh [最长等待秒数，默认不限(推到成功为止)]
set -u

ROOT="$(realpath "$(dirname "$0")/..")"
cd "$ROOT" || exit 1
KEY="$HOME/.ssh/push_ed25519"
REPO="${GITHUB_REPO:-cyqmq/steam302-web}"
REMOTE_SSH="ssh://git@ssh.github.com:443/$REPO.git"
MAX_WAIT="${1:-0}"
START="$(date +%s)"

log() { echo "[$(date '+%H:%M:%S')] $*"; }

head_remote_ssh() {
  timeout 20 env -u http_proxy -u https_proxy \
    GIT_SSH_COMMAND="ssh -i $KEY -o StrictHostKeyChecking=accept-new -o ConnectTimeout=6 -o BatchMode=yes" \
    git ls-remote "$REMOTE_SSH" main 2>/dev/null | awk '{print $1}'
}

push_ssh() {
  timeout 45 env -u http_proxy -u https_proxy \
    GIT_SSH_COMMAND="ssh -i $KEY -o StrictHostKeyChecking=accept-new -o ConnectTimeout=6 -o BatchMode=yes" \
    git push "$REMOTE_SSH" main
}

push_https() {
  timeout 45 git -c http.sslVerify=false push origin main
}

H="$(git rev-parse HEAD)"
while :; do
  RH="$(head_remote_ssh)"
  if [ -n "$RH" ] && [ "$RH" = "$H" ]; then
    log "已同步（远端 main == HEAD $(git rev-parse --short HEAD)）"
    exit 0
  fi

  if push_ssh; then
    log "SSH(443) 推送成功"
    exit 0
  fi
  log "SSH(443) 失败，尝试 HTTPS 加速器…"
  if push_https; then
    log "HTTPS 推送成功"
    exit 0
  fi
  log "两通道本轮均失败"

  if [ "$MAX_WAIT" -gt 0 ] && [ $(( $(date +%s) - START )) -ge "$MAX_WAIT" ]; then
    log "等待 $MAX_WAIT 秒仍无通道，放弃"
    exit 1
  fi
  log "20 秒后重试…"
  sleep 20
done