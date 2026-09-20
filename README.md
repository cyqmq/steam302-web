# steam302-web

把经典工具 [Steamcommunity_302](https://github.com/lyc8503/Steamcommunity_302) **移植到 Linux
/serverside 的 Go 重写版**：数据驱动的规则目录 → 本地 Caddy(TLS MITM) + hosts 劫持 + 用户态端口
转发，配以 systemd 托管与 WebUI 管理，并额外**真正打通了 GitHub 加速**（原版在这台机器上只能回
"空 200"）。

> 移植中的机制、本机实测结论与运维笔记见 [`docs/PORTING.md`](docs/PORTING.md)，
> 规则文件格式说明见 [`docs/RULES.md`](docs/RULES.md)。

## 工作原理

```

浏览器请求 github.com / steamcommunity.com …
   └─ /etc/hosts 劫持 ─► 127.0.0.1:443  ─► s302fwd 用户态转发 ─► 127.0.0.1:25584
                                              （80 ─► 24196）
   ┌────────────────────────────────────────────────────────────「 本地 Caddy MITM
   │  自签 CA + 叶证书（SAN 覆盖全部规则域名）        │
   │  header_up Host {host} · UA 伪装 · SNI 还原      │
   ├─ reverse_proxy ─► str001-004.steam302.xyz （Steam 系）──► 真实 CDN
   ├─ reverse_proxy ─► gh*.steam302.xyz （GitHub 系，钉死可达 IP）──► real GitHub
   └──────────────────────────────────────────────────────────────」
```

## 功能对照总表（与原版 Steamcommunity_302 设置项逐条对照）

### 已实现 ✅

| 原版设置项 | 本仓库实现 |
| --- | --- |
| 自动修改 Hosts | `bin/genhosts apply` / systemd 单元启动前 `genconfig --hosts` 生成；`deploy/apply-hosts.sh` 一键合入 |
| 自动备份 Hosts | `genhosts` 写前备份 `/etc/hosts.S302XBackup`，可 `revert` 回滚 |
| 本地监听 IP（默认 127.0.0.1） | `config/env.json → listen.bind_ip` |
| 监听端口（固定 80/443） | `env.json → fwd.mappings`：`443→25584`、`80→24196` |
| 开机自动运行 | systemd 三单元 `enable`（后台服务模式，等价原版"后台服务(无界面)"） |
| 停止监听 & 禁用代理 | `systemctl stop steam302-web-*` + `genhosts remove`；一键脚本 `deploy/switch-back.sh` / `deploy/uninstall.sh` |
| 重置所有设置（删配置+移自启） | `deploy/uninstall.sh` |
| 证书有效期（如 10 年） | `bin/genpki --ca-years 10 --leaf-days 365` |
| 上游域名（Steam 相关，可自建节点） | 规则文件 `upstreams[]` 覆盖 `env.json → upstream_defaults` 默认值 |
| 用户自定义规则（列表查看） | WebUI `http://127.0.0.1:34902` 列出全部规则 + 启用状态 + 覆盖域名数 + 缺失文件；顶部「一键应用」经 `sudo -n bin/apply` 重生成 → 写 `/etc/hosts` → 重启服务 |
| 查看使用教程 | 仓库内文档：`README` / `docs/RULES.md` / `docs/PORTING.md` |

### 部分实现 ⚠️

| 原版设置项 | 现状 |
| --- | --- |
| 重置根证书 / 重置网站证书（分开重置） | `bin/genpki --reset-root`（仅重置 CA，叶一并重签）/ `--reset-leaf`（保留 CA 只签叶）|
| 启用 DNS 重定向模式 | 本仓库通过 **hosts 劫持**（本质即 DNS 重定向的一种落地）导通流量；**不**劫持 53/UDP，不提供 DNS over TCP 服务 |
| 日志自动清除 | Caddy 日志进 journald（systemd 自动轮转）；`config/s302fwd.log` 按体积轮转（默认 `fwd.log_max_bytes` 5MB，超限改为 `.1`） |
| 输出 DNS 重定向日志 | 未输出按域名请求日志；基础运行日志走 journald / s302fwd.log |
| CDN 优选 | 已实现 `bin/prefer`（node=接入节点 / cf=Cloudflare 段抽样实测），systemd 启动前 `--quick` 补齐缓存并渲染为 Caddyfile 前置 IP；`speed_test` 时按下载速率排序，否则按延迟 |

### 未实现 / 本机不适用 ❌ / ⏹

| 原版设置项 | 说明 |
| --- | --- |
| CDN 优选（Akamai/CF/Fastly 启动测速选最快） | ✅ `bin/prefer`；见上表"CDN 优选"行 |
| 测速速率限制 | ❌（无测速，故无带宽限制） |
| DNS 重定向 CDN 优选 | ❌ 依赖上面的 CDN 优选，未实现 |
| 用户自定义规则编辑器（铅笔图标）/ 域名黑名单 | ✅ WebUI 每条规则「✎ 编辑」直接改 JSON（校验后重生成）；顶部「域名黑名单」面板（`config/blacklist.json`，支持 `*.example.com`，命中域名不进 hosts 劫持走直连） |
| 复制代理设置参数（剪贴板/PAC/环境变量） | ✅ WebUI「复制代理参数」面板：Hosts 劫持片段 / curl 验证命令 / PAC / 环境变量四种格式一键复制；PAC 为**白名单式**（仅被劫持域名走 HTTPS 代理、其余 DIRECT），黑名单域名天然被排除 |
| 界面主题（亮/暗） | ✅ WebUI 顶部切换（CSS 变量 + localStorage 记忆，默认暗色） |
| 自动修改代理（Windows） | ⏹ 不适用：本仓库是 Linux 服务器端；Windows 客户端需自行配置系统代理/PAC |
| 监听端口 & 代理模式（自动配代理联动） | ⏹ 服务端无"自动配置客户端代理"能力 |
| 支持开发者弹窗（每周） | ⏹ GUI 专属，服务器端不适用 |
| 程序启动后自动开启服务 / 自动更新配置 / 退出 UI 同步退后端 / 最小化托盘 | ⏹ GUI 专属；服务端生命周期由 systemd 管理 |

### 本仓库独有的增强

- **GitHub 加速真实可用**：原版对本机只回"空 200"（其 caddy.json 无 github 站点），本仓库新增
  `github_accel` 规则并**将 upstream 钉死为实测可达的节点 IP**，`api.github.com/zen`、github 页、
  raw/avatars 均为真实内容。
- **systemd `Conflicts` 原子切换**：新版三单元与原版 `steam302.service` 互斥，`systemctl start`
  新版会自动停旧版，443/80 无缝交接，避免"劫持生效但 443 无人监听"的断网态。
- **WebUI 分组开关 + 一键重渲染**：`/api/rules`、`/api/rules/{id}`、`/api/regen`。
- **s302fwd 用户态端口转发 daemon**（`run|up|down|status` + pidfile），对标原版 CLI 内核的转发。
- **golden 双向校验**：Go 与 Python 渲染器输出由 4 个 golden 锁定，跑 `go test ./...` 防回归。

## 目录结构

```
bin/                 # 构建产物（gitignore）
config/
  env.json           # 全局环境：监听端口/IP、证书路径、hosts marker、fwd、upstream 默认值
  rules/*.json       # 规则目录：每个文件=一条服务规则（steam/github/discord/youtube…）
  overrides.json     # WebUI 开关覆盖（运行时生成，gitignore）
  rules.schema.json  # 规则 JSON Schema
cmd/                 # genconfig / genhosts / genpki / s302fwd / webui / apply
internal/            # rules / hosts / pki / fwd / webui
deploy/              # install.sh · uninstall.sh · switch-back.sh · apply-hosts.sh
web/files/           # file_server 型服务资源（youtube_iframe 占位，P0）
docs/                # README / RULES.md / PORTING.md
```

## 快速开始

```bash
# 依赖：go 1.21+、caddy（PATH 中）
export PATH=$PATH:/usr/local/go/bin

go build -o bin/genconfig ./cmd/genconfig
go build -o bin/genhosts  ./cmd/genhosts
go build -o bin/genpki    ./cmd/genpki
go build -o bin/s302fwd   ./cmd/s302fwd
go build -o bin/webui     ./cmd/webui
go build -o bin/apply     ./cmd/apply

# 1) 生成证书（自签 CA + 叶证书，SAN 覆盖规则域名）
bin/genpki

# 2) 渲染 Caddyfile + S302.hosts 并 caddy adapt 校验
bin/genconfig --hosts S302.hosts

# 3) 合入 /etc/hosts（需 root）
bin/genhosts apply S302.hosts

# 4) 启动端口转发（80/443 → 24196/25584）：前台
bin/s302fwd run
#    或 daemon 后台：bin/s302fwd up   （down 停止 / status 查看）

# 5) 启动 caddy（detach 注意：用 setsid + 重定向，勿用裸 &）
setsid caddy run --config Caddyfile --adapter caddyfile \
  </dev/null >/tmp/s302_caddy.log 2>&1 &
```

管理台（分组开关，切换即重渲染 + 改写 hosts）：`bin/webui` → <http://127.0.0.1:34902>

## systemd 部署（推荐，开机自启 + 新旧切换）

```bash
sudo bash deploy/install.sh   # 生成 3 个单元：caddy / fwd / webui

# 切到新版（Conflicts 自动停原版 steam302.service）
sudo systemctl start steam302-web-caddy steam302-web-fwd
sudo bash deploy/apply-hosts.sh

# 开机自启（只启新家族；原版与新版不能同时 enable）
sudo systemctl enable steam302-web-caddy steam302-web-fwd steam302-web-webui

# 切回原版 / 完全重置
sudo bash deploy/switch-back.sh    # 回原版
sudo bash deploy/uninstall.sh      # 卸载+恢复默认状态
```

## 验证

```bash
curl -sk https://api.github.com/zen                     # 非空 → GitHub 加速生效
curl -sk https://github.com/ -o /dev/null -w '%{http_code} %{size_download}\n'  # 200 且 >0
curl -sk https://steamcommunity.com/id/steam | wc -c    # >0
curl -sk https://store.steampowered.com/ -o /dev/null -w '%{http_code}\n'
systemctl is-active steam302-web-caddy steam302-web-fwd
ss -tlnp | grep -E ':443 |:25584|:24196'
```

## 测试

```bash
go test ./...        # golden 渲染 + hosts + fwd
go test -race ./internal/fwd/
```

## 已知限制

- `youtube_iframe` 的 `web/files/iframe/iframe_api*` 仍是占位（P0 待实现，见 `docs/RULES.md`）。
- gist.github.com 与部分 github 节点在本机不可达（原版同）；次要 github 站点遇 502 时按
  `docs/PORTING.md` §6 换可达节点。
- WebUI 无鉴权，仅绑 `127.0.0.1`；证书/`bin/`/运行时 hosts 不入库（见 `.gitignore`）。

## 许可

本仓库为学习/移植目的，规则与机制参考 [Steamcommunity_302](https://github.com/lyc8503/Steamcommunity_302)
的思路；请勿用于规避所在地区法律限制。