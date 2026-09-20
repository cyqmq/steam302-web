# 规则目录（Rule Catalog）说明

steam302-web 采用"数据驱动"的规则目录：每个服务（域名集合）是一个独立的 JSON
规则文件，`tools/genconfig.py` 将它们渲染成：

- `Caddyfile`：Caddy 反向代理配置（含 HTTPS MITM 站点、SNI 伪装、负载均衡、头处理）
- `S302.hosts`：hosts 劫持片段（全部域名 → `127.0.0.1`，行尾带 `#S302X` 标记）

思路与 BlockyDeer/CaddyConfig 一致：hosts 把目标域名指到本机 → 本地 Caddy 用自签
证书做 TLS MITM → 按域名/路径反代到真实 CDN（`tls_server_name` 伪装 SNI 为 Akamai
泛域证书、`header_up Host` 还原真实 Host、UA 伪装 Googlebot 绕过 CDN 校验）。

## 目录结构

```
config/
  rules.schema.json   # 规则文件所用 JSON Schema（新增/修改前先对照）
  env.json            # 全局环境：监听端口、证书、hosts marker、上游默认值
  rules/*.json        # 每个文件 = 一条服务规则
  certs/              # genpki 自产 CA/叶证书
  overrides.json      # WebUI 的开关覆盖（可选，运行时生成；不写规则文件）
web/files/            # file_server 型服务（如 youtube_iframe）的静态资源根
tools/genconfig.py    # Python 渲染器（参考实现，golden 校验用）
cmd/genconfig/        # Go 渲染器（主实现）
cmd/genpki/           # 自产 CA + 叶证书
cmd/genhosts/         # apply/remove/revert/status /etc/hosts
cmd/webui/            # 本地 Web 控制台
internal/rules/       # 规则加载 + Caddyfile/hosts 渲染 + golden 单测
internal/pki/         # 证书生成
internal/hosts/       # hosts 文件合并/备份/回滚
internal/webui/       # WebUI HTTP 服务 + 内嵌前端
```

## 一条规则的字段

顶层：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 规则 ID，必须与文件名一致（如 `steam_community`） |
| `enabled` | bool | 是否默认渲染；`--all` 可强制渲染全部 |
| `description` | string | 中文说明（计划用于 WebUI 展示） |
| `hosts` | string[] | 劫持/监听的主机名（**不带**端口与协议） |
| `handlers` | handler[] | 由前到后依次匹配的反代/响应 handler |
| `needs_files` | string[] | 依赖 `web/files/<dir>` 下文件的路径，缺失时告警 |

### handler 类型

- `{ "type": "reverse_proxy" }`
  - `upstreams[]`：静态上游（`https://host`）
  - `dynamic_upstreams[]`：模板上游，如 `a <host> 443`（Caddy `dynamic`）
  - `transport`：`{ "tls": true, "tls_server_name": "..." }` SNI 伪装
  - `lb`：`{ "policy": "random_choose", "count": N, ... }`
  - `header_up[]`：`[name, value]`、`[name, null]`（删除）、`[name, pattern, value]`（正则替换，如 UA）
  - `match`：`{ "path": [...], "uri": [...] }` 命中才走该上游；无 `match` 为兜底
- `{ "type": "respond" }`
  - `match` + `body` / `close`；可带 `status`，可用于空回落（劫持 `api.github.com`)
- `{ "type": "file_server" }`：`root`（相对 `web/`），如 YouTube iframe 脚本
- `{ "type": "redir" }` / `{ "type": "rewrite" }`

Handler 的 `match` 未命中时自动落到下一个 handler；所有站点自动插入
`method OPTIONS → 204`（PNA-CORS 预检）与 `Access-Control-Allow-*` 头。

### env.json

- `listen.https`（默认 25584）/ `listen.http`（默认 24196）/ `bind`（默认 127.0.0.1）
- `cert` / `key`：自签证书路径（MITM 用，校验阶段必须存在）
- `admin.off`、`auto_https.off`
- `hosts_marker`（默认 `#S302X`）：hosts 片段行尾标记与防重复
- `upstream_defaults`：`steam_nodes`/`github_node`/`steam_image_edges` 等公共上游

## 使用

```bash
# 渲染默认启用规则 + 生成 hosts，并用 caddy adapt 校验
python3 tools/genconfig.py --hosts S302.hosts

# 渲染全部规则（含默认关闭，如 github/discord/twitch/pixiv/modio/recaptcha）
python3 tools/genconfig.py --all --hosts S302.hosts

# 只打印不落盘 / 跳过校验（调试）
python3 tools/genconfig.py --print --no-validate
```

要求 `caddy` 在 PATH（校验用 `caddy adapt --validate`）。

证书（自产 PKI）：

```bash
# 生成 CA + 叶证书到 config/certs/（SAN 覆盖全部规则域名）
bin/genpki

# 仅覆盖默认启用规则的域名
bin/genpki --enabled-only
```

hosts 应用（写 `/etc/hosts`，需 root；flags 需放在子命令参数之前）：

```bash
bin/genconfig --hosts S302.hosts          # 先渲染片段
bin/genhosts apply S302.hosts             # 备份后合并写入（幂等，先移除旧 marker 行）
bin/genhosts status                       # 查看生效行
bin/genhosts remove                       # 移除本工具写入的行
bin/genhosts revert                        # 从备份恢复
bin/genhosts apply S302.hosts --dry-run    # 只预览
```

Web 控制台（可视化分组开关；切换即重生成 Caddyfile + hosts）：

```bash
bin/webui                                 # http://127.0.0.1:34902/
bin/webui --addr 0.0.0.0:34902            # 注意：无鉴权，默认只绑本地
```

## 新增一个服务

1. `cp config/rules/steam_cdn_akamai.json config/rules/my_service.json`
2. 改 `id`（=文件名）、`hosts`、`handlers`；涉及 CDN 上游时优先从
   `env.json -> upstream_defaults` 复用节点。
3. `python3 tools/genconfig.py --all --hosts S302.hosts` 直至 `OK`。

## 域名黑名单（不走劫持、直连）

`config/blacklist.json`（WebUI 顶部「域名黑名单」面板编辑），结构 `{"domains": [...]}`：
每项支持精确域名或 `*.example.com` 通配（匹配所有子域名）。命中域名在生成
**Caddyfile 与 hosts 片段**时都会被剔除（整个 site 被清空则不渲染），使这些域名走
真实 DNS 直连，不经过本代理。文件已在 `.gitignore`。同步到 `/etc/hosts` 仍需
`bin/genhosts apply S302.hosts`（或 `systemctl restart steam302-web-caddy` 会在
`ExecStartPre` 重新生成片段后由系统重新合入）。

## 规则在线编辑（WebUI）

WebUI 每条规则卡片的「✎ 编辑」可查看/修改该规则的原始 JSON 并保存：
服务端先做 JSON 解析、`sites` 非空、`id` 与文件名一致、整目录可加载等校验，通过后
写盘、自动重新生成 Caddyfile/hosts 并跑 `caddy adapt` 校验。保留原文件换行格式。

## 已知边界

- `youtube_iframe` 依赖 `web/files/iframe/iframe_api` 与 `iframe_api.js`
  （修复脚本，现为占位，内容待 P0 实现）。
- github 加速（`enabled: true`）：本机无法直连 GitHub，须经 `github_accel` 规则转发到
  gh*.steam302.xyz 节点；节点 DNS 多 IP 混有本机不可达的地址（140.82.x），upstream 已
  钉死为实测可达 IP（维护方式见 `docs/PORTING.md` §6/§7）。gist 本机不可达（原版同）。
- 校验阶段需要证书文件存在于 Caddyfile 同目录。证书由 `cmd/genpki` 自产并写入
  `config/certs/`（`ca.pem/ca.key` + SAN 覆盖全部规则域名的 `leaf.pem/leaf.key`），
  `env.json -> cert` 指针已指向这里。
- 监听仅绑 `127.0.0.1`，与 steam302 一致（真实部署建议结合防火墙）。

## systemd 托管与新旧切换

三个单元：`steam302-web-caddy` / `steam302-web-fwd` / `steam302-web-webui`
（`deploy/install.sh` 生成到 `/etc/systemd/system/`，与机上原版 `steam302.service`
互相 `Conflicts`）。切新版、切回原版、校验清单详见 `docs/PORTING.md` §4/§8。

## CDN 优选（`prefer`）

对启用优选的 site 做测速，选最快 Top-N 写成 IP 前置到 Caddyfile 上游
（IP 直连 + SNI 伪装 Host），原上游保留作 fallback。方法论参考
XIU2/CloudflareSpeedTest：TCP 握手测延迟 → HTTP 下载测速 → 排序。

site 级配置（`config/rules/*.json` 的 `sites[].prefer`）：

```json
"prefer": {
  "mode": "node",                     // node=接入节点/上游主机优选；cf=Cloudflare Anycast IP 优选
  "candidates": ["https://str001.steam302.xyz", "..."],  // node 模式候选（缺省取第一个 reverse_proxy 上游）
  "cidrs": ["104.16.0.0/12", "..."],  // cf 模式：CF 官方段，随机采样
  "samples_per_cidr": 24,             // 每段采样数（cf）
  "top_n": 2,
  "speed_test": true,                 // 是否下载测速
  "download_url": "https://speed.cloudflare.com/__down?bytes=8388608",
  "max_mbps": 20                      // 测速带宽上限
}
```

- 全局默认在 `env.json -> prefer`（`enabled/latency_timeout_ms/parallel/max_mbps/...`）。
- `cf` 模式会以 site 首个域名为 SNI 做一次“真能服务该域名”的校验（能返回任何 HTTP 状态
  才算可用），避免选到连不上该站点的边缘导致 502；并能服务该域名的边缘可能只有极少数，
  采样抽空会翻倍重试（24→48）。`cf` 上游还会自动 `tls_insecure_skip_verify`
  （steamstatic 的 Cloudflare 边缘证书 2025-10-01 过期，且客户端侧仍由本地 CA 全链路 MITM）。
- `node` 模式默认不做下载测速（接入节点不支持任意 SNI），按延迟排序。
- 缓存写在 `config/prefer.json`（已 gitignore）。

命令：

```bash
bin/prefer run --timeout 90 --debug     # 全量：抽样→延迟→域名校验→下载测速→写缓存
bin/prefer run --quick                  # 只补缺失项、不覆盖已有（开机用，避免冲掉全量结果）
bin/prefer show | bin/prefer clear      # 查看 / 清除缓存
```

systemd 的 caddy 单元 `ExecStartPre` 已挂 `prefer run --quick --timeout 12`，
保证每次启动先用上一轮优选结果渲染（缺失项补齐），再生成 Caddyfile。