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
   ├─ reverse_proxy ─► gh*.steam302.xyz （GitHub 系，多候选 + 优选 IP）──► real GitHub
   └──────────────────────────────────────────────────────────────」
```

## 当前状态小结

| 维度 | 结论 |
| --- | --- |
| 我们已实现 | 原版 Steamcommunity_302 的**机制全部移植**（hosts 劫持 + Caddy TLS MITM 反代 + 自签证书 + 80/443 转发），覆盖原版**绝大多数设置项**（见下表「已实现 ✅」），并新增 11 项专项规则（全默认关闭）、**真实可用的 GitHub 加速**、CDN/IP 自动优选（`prefer` + `fetchghip` + `fetchcdnips`）、本机 DNS 重定向（`dnsd`）、systemd 原子切换、WebUI 设置页 |
| 还差什么 | **DNS 重定向模式的 IP 优选**未做（`dnsd` 只按域应答 `127.0.0.1`）、`youtube_iframe` 修复脚本仍是占位（P0）、`输出 DNS 重定向日志` 未输出按域名请求日志，**Windows 自动改代理 / 开发者弹窗 / 托盘**等 GUI 专属项服务端不适用 |
| 比原版多了什么 | **GitHub 加速真实可用**（原版本机只回"空 200"）、`github` 系与 Steam 系 CDN 的**自动测速优选 + 多候选 failover**、WebUI 分组开关/编辑/一键应用、`s302fwd` 用户态转发 daemon、golden 双向校验防回归、`dnsd` 本机 DNS 重定向替代纯 hosts 劫持 |

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
| 用户自定义规则编辑器（铅笔图标）/ 域名黑名单 | ✅ WebUI 每条规则「✎ 编辑」直接改 JSON（校验后重生成）；顶部「域名黑名单」面板（`config/blacklist.json`，支持 `*.example.com`，命中域名不进 hosts 劫持走直连） |
| 复制代理设置参数（剪贴板/PAC/环境变量） | ✅ WebUI「复制代理参数」面板：Hosts 劫持片段 / curl 验证命令 / PAC / 环境变量四种格式一键复制；PAC 为**白名单式**（仅被劫持域名走 HTTPS 代理、其余 DIRECT），黑名单域名天然被排除 |
| 界面主题（亮/暗） | ✅ WebUI 顶部切换（CSS 变量 + localStorage 记忆，默认暗色） |
| 查看使用教程 | 仓库内文档：`README` / `docs/RULES.md` / `docs/PORTING.md` |
| Origin 游戏下载（HTTPS→HTTP） | `origin_dl` 规则（默认关）：origin-a.akamaihd.net 反代到 HTTP 流媒体边缘 |
| Uplay 客户端更新防劫持 | `uplay_update` 规则（默认关）：static3.cdn.ubi.com 转回官方源 |
| EA Desktop CDN 重定向 Akamai | `ea_desktop` 规则（默认关，支持 CEL `expression` 匹配）：downloadURL 追加 `cdnOverride=akamai` |
| HB / Fanatical 图片 | `hb_fanatical_imgfix` 规则（默认关）：反代 imgix 官方 Fastly 边缘 |
| Fandom 图片 | `fandom_imgfix` 规则（默认关）：wikia.nocookie.net 全子域反代官方源 |
| OneDrive 网页版 | `onedrive_web` 规则（默认关）：主页 + skyapi 反代微软 edge IP 池 |
| Blockbench | `blockbench` 规则（默认关）：官网反代其托管、web 编辑器走 ghps 节点 |
| jsDelivr CDN | `jsdelivr` 规则（默认关）：反代官方 Fastly 边缘 |
| CSGO(CS2) Demo 国转国际 | `csgo_demo_redir` 规则（默认关）：replay.csgo.com.cn 302 到官方国际节点 |
| 辐射76 登录修复 | `fallout76_respond` 规则（默认关）：区域 httpping 端点本地应答 200 |
| Xbox 云游戏 / 商店图片 | `xbox_cloud` 规则（默认关）：gssv-play-prod 跨区反代 + 商店图片反代 |

### 部分实现 ⚠️

| 原版设置项 | 现状 |
| --- | --- |
| 重置根证书 / 重置网站证书（分开重置） | ✅ WebUI「设置」页按钮（内部 `genpki --reset-root`/`--reset-leaf`，重置后自动重载）；CLI 亦可 |
| 证书有效期 | ✅ WebUI「设置」页可改 CA 年数/叶天数（写 `env.json → cert.ca_years/leaf_days`，genpki 未带 flag 时读之；默认 10 年 / 365 天）|
| 启用 DNS 重定向模式 | 默认走 **hosts 劫持**；另提供轻量本机 DNS 重定向 `bin/dnsd`（`steam302-web-dnsd.service` 可选）：劫持域应答 `127.0.0.1`、其余域名转发上游（支持 `*.mod.io` 这类 hosts 无法表达的通配子域），监听 `127.0.0.1:53` UDP+TCP，带上游 TTL 缓存；需时将系统解析器指向 `127.0.0.1` 启用 |
| 日志自动清除 | Caddy 日志进 journald（systemd 自动轮转）；`config/s302fwd.log` 按体积轮转（WebUI「设置」页可调 `fwd.log_max_bytes`，默认 5MB，超限改为 `.1`） |
| 输出 DNS 重定向日志 | 未输出按域名请求日志；基础运行日志走 journald / s302fwd.log |
| CDN 优选 | 已实现 `bin/prefer`（node=接入节点 / Akamai 镜像、cidr/cf=官方段抽样实测），systemd 启动前 `--quick` 补齐缓存并渲染为 Caddyfile 前置 IP；cidr/cf 只保留经 2xx 域名校验的 pins，避免 403/502；`speed_test` 时按下载速率排序，否则按延迟；github 系站点由 `bin/fetchghip` 生成候选后走同一优选流程（见「本仓库独有的增强」） |
| CDN 优选段库 | `config/cdn_ips.json` vendored 官方段（Akamai/Cloudflare/Fastly/CloudFront），`bin/fetchcdnips` 可拉取更新（详见 `docs/RULES.md`） |
| 监听 IP / 测速限速 / 备份保留数量 | ✅ WebUI「设置」页：`listen.bind_ip`、`prefer.max_mbps`（当前 20）与并行/采样等数值、`hosts.backup_keep`（快照保留数，0=不清理） |
| 开机自启 & 重置所有设置（恢复出厂） | ✅ WebUI「设置」页：开机自启开关（systemctl enable/disable 三服务）；「恢复出厂设置」＝ `bin/reset`（停服＋撤自启＋撤销 hosts 劫持＋清证书/生成物，**保留**规则与 env.json） |

### 未实现 / 本机不适用 ❌ / ⏹

| 原版设置项 | 说明 |
| --- | --- |
| DNS 重定向 CDN 优选 | ❌ 未实现（`bin/dnsd` 只按域应答 `127.0.0.1`，不做 IP 优选） |
| 自动修改代理（Windows） | ⏹ 不适用：本仓库是 Linux 服务器端；Windows 客户端需自行配置系统代理/PAC |
| 监听端口 & 代理模式（自动配代理联动） | ⏹ 服务端无"自动配置客户端代理"能力 |
| 支持开发者弹窗（每周） | ⏹ GUI 专属，服务器端不适用 |
| 程序启动后自动开启服务 / 自动更新配置 / 退出 UI 同步退后端 / 最小化托盘 | ⏹ GUI 专属；服务端生命周期由 systemd 管理 |

### 本仓库独有的增强

- **GitHub 加速真实可用**：原版对本机只回"空 200"（其 caddy.json 无 github 站点），本仓库新增
  `github_accel` 规则，`api.github.com/zen`、github 页、raw/avatars 均为真实内容。
  上游为**多候选 + 自动优选**：`bin/fetchghip` 幂等重写 `prefer.candidates`
  （固定种子 + GitHub 官方段过滤），`bin/prefer --rule github_accel` 实测择优后渲染为
  Caddyfile 前置 IP，原 handler 上游保留兜底，任一 pin 失效由 caddy 健康检查剔除
  （详见 `docs/RULES.md` §GitHub 域名优选）。
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
cmd/                 # genconfig / genhosts / genpki / s302fwd / webui / apply / reset / prefer / fetchghip / fetchcdnips / dnsd
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
go build -o bin/reset     ./cmd/reset
go build -o bin/prefer     ./cmd/prefer
go build -o bin/fetchghip   ./cmd/fetchghip
go build -o bin/fetchcdnips ./cmd/fetchcdnips
go build -o bin/dnsd       ./cmd/dnsd

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

## 参考仓库

本项目是以下仓库/思路的组合移植与演进，借用了各自的核心机制：

| 仓库 | 参考点 | 本仓库对应实现 |
| --- | --- | --- |
| [lyc8503/Steamcommunity_302](https://github.com/lyc8503/Steamcommunity_302) | 原版工具：hosts 劫持 + Caddy MITM 反代 + 自签证书 + 监听 80/443 转发 | 全仓规则目录/证书/转发体系即为该思路的 Go 重写 |
| [BlockyDeer/CaddyConfig](https://github.com/BlockyDeer/CaddyConfig) | 纯 Caddy 版 steamcommunity302（把 feature 续进 caddy.json，含 hosts、UA 伪装、SNI 还原） | Caddyfile 渲染、`tls_server_name` SNI 伪装、`header_up Host {host}` 的直接出处 |
| [Chinachani/steam-hosts-tools](https://github.com/Chinachani/steam-hosts-tools) | DoH 防污染解析（DNSPod/AliDNS）+ TCP 443 并发测速优选 + hosts 备份/清洗 + Steam 域名清单 | `internal/prefer`（DoH 兜底、TCP 延迟排序——两者 DoH 源一致）、`internal/hosts`（快照） |
| [mansourjabin/cdn-ip-database](https://github.com/mansourjabin/cdn-ip-database) | 厂商直发布的 CDN 段（Akamai/Cloudflare/Fastly/CloudFront…，daily resolved_ips.json） | `config/cdn_ips.json` vendored 段库 + `bin/fetchcdnips` 更新器（cidr 优选的数据源） |
| [jitre/steam_hosts](https://github.com/jitre/steam_hosts)（fork 自 [fordes123/hosts_generator](https://github.com/fordes123/hosts_generator)） | GitHub Action + ECS DNS（DoH 带客户端子网）按区域解析出最优 Steam IP 生成 hosts | 区域自愈思路可对照：本仓用 `bin/prefer` 在本机实测代替 ECS 解析 |
| [oldj/SwitchHosts](https://github.com/oldj/SwitchHosts) | 跨平台 hosts 管理（分组切换/远程 hosts/定时刷新/本地 HTTP API） | 与 `bin/webui` + `bin/genhosts` 的"分组开关 + 一键应用"功能等价，适合作为**客户端机器**上的 hosts 管理向工具 |
| [4n0nymou3/Clean-IP-Scanner](https://github.com/4n0nymou3/Clean-IP-Scanner) | Termux/Xray 场景按 CDN 段扫描"干净 IP"（TCPing+下载测速+提交续扫） | cidr 模式采样/校验思路同源；其扫描仅为找到可连边缘，本仓再叠加"能 2xx 服务该域名"校验 |
| [XIU2/CloudflareSpeedTest](https://github.com/XIU2/CloudflareSpeedTest) | TCP 握手测延迟 → HTTP 下载测速 → 排序（CDN 优选方法论） | `internal/prefer` 的探测流水线直接沿用该思路 |

> 另外两类相关但未直接采用的思路值得关注：
> - **自建接入节点**（`str*.steam302.xyz` 类）：上游 hosts-only 加速器运维方自建的反代节点池，本仓通过 `node` 模式优选接入（`env.json → upstream_defaults`）。
> - **SwitchHosts 式远程 hosts 订阅**：在本机部署场景下可由用户手动配置 SwitchHosts 拉取 `S302.hosts` 同步到其它设备，作为对 WebUI 的补充。

## 已知限制

- `youtube_iframe` 的 `web/files/iframe/iframe_api*` 仍是占位（P0 待实现，见 `docs/RULES.md`）。
- gist.github.com 本机不可达（原版同）：`fetchghip` 自动剔除被污染的 gist 候选（不在 GitHub
  官方段），交由 handler 上游 `ghgist.steam302.xyz` 兜底；次要 github 站点遇 502 时由
  caddy 健康检查剔除失效 pin 自动切换（多候选 + 兜底上游）。
- WebUI 无鉴权，仅绑 `127.0.0.1`；证书/`bin/`/运行时 hosts 不入库（见 `.gitignore`）。

## 许可

本仓库为学习/移植目的，规则与机制参考 [Steamcommunity_302](https://github.com/lyc8503/Steamcommunity_302)
的思路；请勿用于规避所在地区法律限制。