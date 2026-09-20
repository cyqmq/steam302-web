# 移植笔记：Steamcommunity_302 → steam302-web

本文件记录从原版 [Steamcommunity_302] 移植到本仓库（Go 重写）过程中的全部实测结论、
架构推演与运维经验。目标读者：接手这台机器 / 继续开发本仓库的人。

## 1. 本机现状（2026-09-20 版本）

- 操作系统 Ubuntu（systemd），root。项目位于 `/root/.local/steam302-web`。
- 原版发行目录 `/home/huidtose/Steamcommunity_302/`（含 `steamcommunity_302.cli`、
  `Caddy` 可执行文件、`S302_rules.ini`、`steamcommunity_302.caddy.json`、`S302.hosts`、`S302.log`）。
- 最终生效态 = **本仓库（新版）**：
  - `steam302-web-caddy.service` + `steam302-web-fwd.service` + `steam302-web-webui.service`
    已 enable（开机自启）；
  - 原版 `steam302.service` 已 disable（其与新版单元 `Conflicts`，任一方启动自动停另一方）；
  - `/etc/hosts` 已合入 98 行 `#S302X` 劫持行（steam 全家桶 + github 全家桶）。
- 网络事实：本机**无法直连 GitHub**（`curl --resolve` 打官方 IP 超时），必须走代理；Steam 官方
  CDN 直连同样不通；而 `*.steam302.xyz` 各节点此时**可达**（曾有过不可达窗口，见 §6）。

## 2. 原版工作机制（逆向结论）

```
浏览器 → github.com / steamcommunity.com (DNS→127.0.0.1) → 127.0.0.1:443
        → steamcommunity_302.cli 的「端口转发」内核
        → 127.0.0.1:27030 本地 Caddy（steamcommunity_302.caddy.json）
        → 反代 + 自签证书 MITM + UA 伪装 Googlebot + 按 {host} 还原 Host
        → str001-004.steam302.xyz 转发节点 → 真实 CDN/源站
```

- 监听：
  - `.cli` 用户态转发：`127.0.0.1:443 → 127.0.0.1:27030`，`127.0.0.1:80 → 127.0.0.1:29508`。
  - 本地 Caddy 直接监听 `*:27030` / `*:29508`（非标准端口，避免占用 443/80 需特权）。
- 每站点带 `tls_server_name <目标域名>`（SNI 伪装成真实 CDN 域名，让 Akamai/CDN 放行），
  细节见 `steamcommunity_302.caddy.json`。
- 规则库 `S302_rules.ini`：每节 `[service]` = `Domain=` 域名列表 + `Json=`（**XOR 加密的
  caddy 站点片段**）；`enabled` 行列出全部可用规则（注意这是"能力清单"，不等于全量生成）。
- hosts：原版启动时把 steam 系列域名写进 `/etc/hosts`（无标记）。**而 github 域名块是有旧注释
  `# steam302 github acceleration (manual)` 的"手动"块，无标记、`genhosts` 清不掉。**
- systemd：`steam302.service`，`ExecStart=…/steamcommunity_302.cli`，`Restart=always`
  （所以 `systemctl stop` 会被拉起，必须再 `disable`）。

### 2.1 原版对 github 是"空 200"假象

`curl -sk https://api.github.com/zen` 返回 **HTTP 200、body 0 字节**。原因是 github 域名被
劫持到本地 Caddy，但该 Caddy 配置里**根本没有 github 站点**，未匹配站点 → Caddy 默认
"200 空 body"。因此**本机 GitHub 一直就没真正通**（浏览器白屏、git/IDE API 空响应报
"Cannot connect to API"）。这份笔记之前的所有排查里，"github 能用/不能用"都要以
**能否拿到 >0 字节正文**为准，不能只看状态码。

## 3. 本仓库（新版）设计

- **数据驱动规则目录** `config/rules/*.json`（每个服务一个文件，含 `enabled` 默认开关），
  Go 渲染器 `cmd/genconfig`（`internal/rules/render.go`）→ `Caddyfile` + `S302.hosts`；
  另有 Python 版 `tools/genconfig.py` 作为参考实现，**4 个 golden 以 Go 输出为权威**。
- 端口：`env.json` 里 `listen.https=25584`、`listen.http=24196`（不占 443/80），
  `bind=127.0.0.1`。**对标原版 "cli 转发 443/80→内部端口" 的设计，我们实现了
  `internal/fwd`（cmd/s302fwd）**：用户态 TCP 端口转发 `80 → 24196`、`443 → 25584`，
  带 pidfile/daemon（up/down/status）。
- PKI：`cmd/genpki` 自产 CA + 叶证书（SAN 覆盖全部规则域名）到 `config/certs/`，MITM 用。
- hosts 管理：`cmd/genhosts`（apply/remove/revert/status，行尾标记 `#S302X`，备份
  `/etc/hosts.S302XBackup`）。
- CDN 优选：`bin/prefer`（`internal/prefer`）测速选最快，写成 IP 前置到 Caddyfile 上游，
  缓存 `config/prefer.json`（gitignore）；caddy 单元 `ExecStartPre` 挂
  `prefer run --quick --timeout 12`（只补缺失、不覆盖已有）。详见 `docs/RULES.md` §CDN 优选。
- 域名黑名单：`config/blacklist.json`（WebUI「域名黑名单」面板，支持 `*.example.com`），
  生成时剔除命中域名不渲染（`internal/rules/blacklist.go`）。
- WebUI：`bin/webui`（127.0.0.1:34902，无鉴权，勿绑外网），支持主题切换、规则在线
  编辑、代理参数复制、域名黑名单。

### 3.1 与原版的能力等价性（已实测）

| 服务 | 原版 | 新版 |
| --- | --- | --- |
| steamcommunity / store / steamstatic CDN | ✓ | ✓ |
| github 全家桶 | ✗ 空 200 | ✓ 真实内容（钉死节点） |
| 443/80 生命周期 | systemd 托管 | systemd 托管（新版三单元） |
| 管理 UI | 原版 GUI | WebUI |

## 4. systemd 切换战术

三个单元：`steam302-web-caddy.service`（`Conflicts=steam302.service`，运行 user=root，
`ExecStartPre=bin/genconfig --hosts` 保证配置与规则开关同步，`ExecStart=caddy run …`）、
`steam302-web-fwd.service`（`Conflicts=steam302.service`，`ExecStart=bin/s302fwd run`，
`After=caddy`）、`steam302-web-webui.service`（34902）。

双向切换（`Conflicts` 自动停对方，443/80 无缝交接）：

```bash
# 切到新版
systemctl start steam302-web-caddy steam302-web-fwd
bash deploy/apply-hosts.sh          # 写入 #S302X 行
# 切回原版
bash deploy/switch-back.sh          # 停新版 → genhosts remove → enable+start 原版
```

**两边不能同时 `enable`**（开机两人互杀）；当前已把原版 disable、新版 enable。

## 5. 事故复盘（网络"用不了"的真实链条）

1. 我们最初 `systemctl stop 并 disable steam302.service`，随后起新版（caddy 裸监听
   25584/24196、s302fwd 转发 443/80）并手动跑通。
2. 机器重启（20:32）。**新版没有任何 unit，全灭**；原版已被 disable 也没起。
   → 443/80 无人监听。而 `/etc/hosts` 里 github/steam 全被劫持到 127.0.0.1。
   → 所有 github 请求 `127.0.0.1:443` 连接被拒 → "Cannot connect to API / Unable to connect"。
3. `systemctl enable --now steam302.service` 回滚到原版后，steam 恢复、github 变回"空 200"。
   **结论：不是我们改网络配置弄坏了网，是"劫持已生效但 443 没有服务"这个空档。**

教训（已写进 §4 战术）：
- **劫持在，443 必须有人在。** 停服务前先建造全套替代，或用 systemd Conflicts 原子交接。
- 新版服务必须 systemd 托管 + enable，否则一次重启即断。

### 5.1 测试工具链的坑

- `curl --resolve host:25584:127.0.0.1 https://host/` **不会改端口**，URL 不带 `:25584`
  仍打 443 →（原版在位时）空 200，曾误判"我们 caddy github 空"。正确写法：
  `https://host:25584/`。
- 判定 github 真通：检查 `size_download > 0`。
- 起 caddy 后台要用 `( setsid caddy run … </dev/null >/tmp/s302_caddy.log 2>&1 & )`；
  `pkill -f "caddy run …"` 的 pattern 会**自匹配当前 shell 的命令行**把整个 bash 杀掉
  （实测把工具会话挂死 120s）。

## 6. 节点可达性（本机实测）

| 用途 | 节点 | DNS 现解析（本机） | 实测可达 |
| --- | --- | --- | --- |
| github 主站 / www | `ghgist.steam302.xyz` | 20.27.177.113, **20.205.243.166**, 140.82.114.3/4 | **仅 20.205.243.166** |
| gist | `ghgist` 同上 | 同上 | 均不可达（原版同） |
| api | `ghapi.steam302.xyz` | 140.82.114.5, **20.27.177.116**, 140.82.121.6, 140.82.112.6, 20.205.243.168 | 20.27.177.116 / 20.205.243.168 |
| raw / avatars / usercontent | `ghraw` | 185.199.109/110/108/111.133 | 185.199.109.133 等全通 |
| githubassets(154) / ghio / ghps | 185.199.*.153/.154 | 同段 | 通 |
| codeload | `ghcload` | 140.82.114.10 等 + **20.205.243.165** | 20.205.243.165 |
| steam 转发 | str001-004.steam302.xyz | — | 曾不可达，现 140.245.84.81 可达 |

要点：
- **节点按 HTTP Host 头路由，不按 SNI 路由**（`Host: github.com` 即返回真 github 内容，
  SNI 随意）。因此 `header_up Host {host}` 是灵魂，`tls_server_name` 那里的 `{host}`
  是否替换其实无关紧要。
- **DNS 多 IP 混入本机黑洞（140.82.x 被墙）**：Caddy 每连接随机挑 IP，撞死 IP 就挂起。
  对策：`github_accel.json` 的 upstream 已**钉死为 §6 表中"实测可达"的 IP**（改节点时更新）。
- 裸 `gh.steam302.xyz` 在本机 **NXDOMAIN**（原规则用它当 github 主站，属配置错误，已改）。
- 新增规则时若某服务只能走域名的多 IP，请先逐 IP 实测再钉。

## 7. 遗留边界 / TODO

- `youtube_iframe`：`web/files/iframe/iframe_api*` 仍为占位（66B/116B 响应），P0 未做。
- gist.github.com 本机不可达（原版同样）；次要 github 系（gh140 段 support/education 等）
  未完整实测，遇 502 时参考 §6 换可达节点。
- WebUI 无鉴权，仅绑 127.0.0.1。
- `/etc/hosts` 里旧的手动 github 块（约 21 行，无 `#S302X` 标记）与原 github 块重复，
  `genhosts remove` 清不掉——目标都是 127.0.0.1:443，重复无害，可手动清理。
- 节点 IP 日后变动：改 `config/rules/github_accel.json` 里钉死的 upstream + 请同步 §6。

## 8. 校验清单（切换后必跑）

```bash
curl -sk https://api.github.com/zen                       # 非空
curl -sk https://github.com/ -o /dev/null -w '%{http_code} %{size_download}\n'   # 200 且 >0
curl -sk https://steamcommunity.com/id/steam | wc -c     # >0
curl -sk https://store.steampowered.com/ -o /dev/null -w '%{http_code}\n'
systemctl is-active steam302-web-caddy steam302-web-fwd 
ss -tlnp | grep -E ':443 |:25584|:24196'
```