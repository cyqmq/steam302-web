<script setup>
import { ref, computed, inject, onMounted } from 'vue'
import {
  Pencil, ListPlus, CheckSquare, Square, RefreshCw, Power, Search, X, List, Globe, Network
} from 'lucide-vue-next'
import ToggleSwitch from '../components/ToggleSwitch.vue'
import EditorModal from '../components/EditorModal.vue'
import { get, post } from '../lib/api.js'
import { store, toast } from '../lib/state.js'

const reload = inject('reload', async () => {})
const reloadStatus = inject('reloadStatus', async () => {})
const goSettings = inject('goSettings', () => {})

// —— 网络接管模式面板（对齐原版 mode-panel；「详细设置」跳转到设置对应分区）——
const modes = computed(() => [
  {
    key: 'hosts',
    icon: Pencil,
    title: 'Hosts 模式',
    desc: '为需要代理的域名写入 127.0.0.1 hosts 条目，支持泛域名与全局 CDN 优选（仅对代理连接生效）。',
    on: !!store.status?.hosts_on,
    sub: 'network'
  },
  {
    key: 'dns',
    icon: Globe,
    title: 'DNS 重定向模式',
    desc: '通过驱动或防火墙接管系统 DNS 查询，并由 S302 按规则返回解析结果。支持泛域名及全局 CDN 优选。',
    on: !!(store.status?.dns_redirect || (store.dns && store.dns.active)),
    sub: 'network'
  },
  {
    key: 'proxy',
    icon: Network,
    title: '系统代理模式',
    desc: '自动修改系统代理仅在桌面端可用；浏览器端可下载 proxy.pac 手动配置，按同样规则分流。',
    on: false,
    sub: 'network'
  }
])

// 功能卡映射：id → { g(分组), t(标题), d(说明) }
const FEAT = {
  steam_store: { g: 'steam', t: 'Steam 商店', d: '加速/修复 Steam 商店访问。' },
  steam_community: { g: 'steam', t: 'Steam 社区解锁', d: '解锁被限制的社区内容。' },
  steam_chat: { g: 'steam', t: 'Steam 好友聊天 图片发送修复', d: '修复聊天中图片无法加载/发送的问题。' },
  youtube_iframe: { g: 'steam', t: 'Steam 创意工坊大图修复', d: '修复创意工坊图片无法显示的问题。' },
  steam_cdn_akamai: { g: 'steam', t: 'Steam 网页布局/图片修复(CF+Akamai)', d: '修复 Steam 网页排版错乱和图片加载（针对 Cloudflare 和 Akamai CDN）。' },
  steam_cdn_cloudflare: { g: 'steam', t: 'Steam 网页布局/图片修复(CF+Akamai)', d: '修复 Steam 网页排版错乱和图片加载（针对 Cloudflare 和 Akamai CDN）。' },
  steam_cdn_fastly: { g: 'steam', t: 'Steam 网页布局/图片修复(Fastly)', d: '修复 Steam 网页排版错乱和图片加载（针对 Fastly CDN）。' },
  steam_cloud_ugc: { g: 'steam', t: 'Steam 云同步(仅Google)', d: '修复使用 Google 服务的 Steam 云存档同步问题。' },
  ea_desktop: { g: 'ea', t: 'EA下载重定向Akamai', d: '将 EA 下载流量重定向至 Akamai CDN，提升下载速度。' },
  google_recaptcha: { g: 'other', t: 'Google 验证码', d: '解决 Google reCAPTCHA 验证码加载失败问题。' },
  discord_accel: { g: 'other', t: 'Discord 语音', d: '优化 Discord 语音连接。' },
  twitch_accel: { g: 'other', t: 'Twitch 直播', d: '加速 Twitch 直播观看。' },
  modio: { g: 'other', t: 'Mod.io', d: '加速 Mod.io 网站/服务访问。' },
  github_accel: { g: 'other', t: 'Github', d: '加速 GitHub 网页及资源加载。' },
  blockbench: { g: 'other', t: 'Blockbench', d: '加速 Blockbench（3D建模软件）相关服务。' },
  fandom_imgfix: { g: 'other', t: 'Fandom 图片修复', d: '修复 Fandom 维基百科的图片加载。' },
  onedrive_web: { g: 'other', t: 'OneDrive 网页版', d: '加速 OneDrive 网页版访问。' },
  jsdelivr: { g: 'other', t: 'jsDelivr', d: '加速 jsDelivr CDN 资源加载。' },
  fallout76_respond: { g: 'other', t: '辐射76 登录修复(1:5:1)', d: '解决《辐射76》登录问题（特定比例修复）。' },
  csgo_demo_redir: { g: 'other', t: 'CSGO(CS2) Demo录像下载国区转国际', d: '解决 CS2 国区无法下载 Demo 录像的问题。' },
  uplay_update: { g: 'other', t: 'Uplay 下载CDN重定向国内', d: '将 Uplay（Ubisoft Connect）下载重定向至国内 CDN。' },
  xbox_cloud: { g: 'other', t: 'XBOX 下载CDN重定向国内', d: '将 Xbox 下载重定向至国内 CDN，提升下载速度。' },
  origin_dl: { g: 'other', t: 'Origin 游戏下载（HTTPS→HTTP）', d: 'origin-a.akamaihd.net：反代到 HTTP 流媒体边缘绕过高昂 HTTPS 出口。' },
  pixiv_accel: { g: 'other', t: 'Pixiv 直连', d: 'pixiv 全系主站/API 直连日本源站，图片走专用节点。' },
  hb_fanatical_imgfix: { g: 'other', t: 'HB / Fanatical 图片修复', d: '修复 Humble Bundle / Fanatical 商店图片加载。' },
  x_accel: { g: 'more', t: 'X / Twitter 加速', d: 'X(Twitter) 全系域名走 CF 优选边缘直连。' },
  epic_accel: { g: 'more', t: 'Epic 商城/启动器加速', d: 'Epic 商店/启动器/下载走优选边缘直连。' },
  reddit_accel: { g: 'more', t: 'Reddit 加速', d: 'Reddit 主站/图片走 Fastly 优选边缘直连。' },
  instagram_accel: { g: 'more', t: 'Instagram 加速', d: 'Instagram 主站/图片走 Meta 边缘优选直连。' },
  wikipedia_accel: { g: 'more', t: '维基百科加速', d: 'Wikipedia/Wikimedia 走 CF 优选边缘直连。' }
}

const SECT = [
  { key: 'steam', label: 'Steam 卡片' },
  { key: 'ea', label: 'EA 卡片' },
  { key: 'other', label: '其他服务' },
  { key: 'more', label: '更多可加速服务（默认关闭）' }
]

const q = ref('')
const secs = computed(() => {
  const kw = q.value.trim().toLowerCase()
  return SECT.map((sec) => {
    const items = store.rules.map((r) => {
      const m = FEAT[r.id] || {}
      return {
        rule: r,
        g: m.g || 'other',
        t: m.t || r.name || r.id,
        d: m.d || r.description || ''
      }
    }).filter((x) => x.g === sec.key)
      .filter((x) =>
        !kw ||
        x.t.toLowerCase().includes(kw) ||
        x.d.toLowerCase().includes(kw) ||
        (x.rule.name || '').toLowerCase().includes(kw) ||
        x.rule.id.toLowerCase().includes(kw)
      )
    return { ...sec, items }
  }).filter((s) => s.items.length)
})

const total = computed(() => store.rules.length)
const enabled = computed(() => store.rules.filter((r) => r.enabled).length)

const st = computed(() => store.status || {})

const editor = ref(null)
const openEditor = (id) => (editor.value = { mode: 'rule', id })
const openBlacklist = () => (editor.value = { mode: 'blacklist' })

const domainsModal = ref(null)
async function viewDomains(rule) {
  try {
    const d = await get('/api/rule/' + rule.id)
    let hosts = []
    try {
      const obj = JSON.parse(d.text)
      hosts = (obj.sites || []).map((s) => (s.hosts || []).join('\n')).join('\n')
    } catch {
      hosts = [...(d.sites || [])]
    }
    domainsModal.value = { id: rule.id, name: rule.name, hosts: hosts.split('\n').filter(Boolean) }
  } catch (e) {
    toast('加载域名列表失败: ' + e.message, 'err')
  }
}

async function toggleRule(rule, v) {
  try {
    await post('/api/rules/' + rule.id, { enabled: v })
    toast((v ? '已启用 ' : '已停用 ') + rule.id)
    reload()
  } catch (e) {
    toast('操作失败: ' + e.message, 'err')
  }
}

async function bulkAll(on) {
  try {
    await post('/api/rules/bulk', { enabled: on })
    toast(on ? '已全部启用' : '已全部停用')
    reload()
  } catch (e) {
    toast('操作失败: ' + e.message, 'err')
  }
}

async function stopAll() {
  try {
    const d = await post('/api/services/stop', {})
    const arr = d?.stopped || []
    toast(arr.length ? '已停止服务: ' + arr.join(', ') : '（无运行中服务）')
    reload()
  } catch (e) {
    toast('停止失败: ' + e.message, 'err')
  }
}

async function startAll() {
  try {
    const d = await post('/api/apply', {})
    if (d && d.applied) {
      toast('已启动服务并应用配置')
    } else {
      toast((d && (d.error || d.hint)) || '启动失败', 'err')
    }
    reload()
    loadDns()
  } catch (e) {
    toast('启动失败: ' + e.message, 'err')
  }
}

async function regen() {
  try {
    const d = await post('/api/regen', {})
    toast('已重新生成配置' + (d?.regen?.ok ? '' : '（部分告警）'))
  } catch (e) {
    toast('生成失败: ' + e.message, 'err')
  }
}

async function refreshAll() {
  await reloadStatus()
  await loadDns()
}

function unitCN(s) {
  if (!s) return '-'
  const m = {
    active: '运行中',
    activating: '启动中',
    inactive: '已停止',
    failed: '失败',
    deactivating: '停止中'
  }
  return m[s] || s
}

// DNS 详情快照
const dnsSnap = ref(null)
async function loadDns() {
  try {
    dnsSnap.value = await get('/api/dns')
  } catch {
    dnsSnap.value = null
  }
}
onMounted(loadDns)
const dnsDetail = computed(() => {
  const d = dnsSnap.value
  if (!d) return []
  const rows = [
    { k: 'DNS 服务', v: d.active ? '运行中' : '已停止' },
    { k: '监听地址', v: d.listen || '-' }
  ]
  if (d.upstream && d.upstream.length) rows.push({ k: '上游 DNS', v: d.upstream.join(', ') })
  if (d.ttl) rows.push({ k: 'TTL (秒)', v: String(d.ttl) })
  if (d.answer_ip) rows.push({ k: '应答 IP', v: d.answer_ip })
  rows.push({ k: '自定义解析', v: d.user_rules ? '已启用' : '关闭' })
  rows.push({ k: '查询日志', v: d.query_log ? '已启用' : '关闭' })
  rows.push({ k: '系统解析器接管', v: d.resolv_managed ? '已接管' : '未接管' })
  rows.push({ k: '局域网 53 重定向', v: d.lan_redirect ? '已生效' : '未生效' })
  return rows
})

const vitals = computed(() => {
  const s = st.value
  const up = (s.upstream_domains && s.upstream_domains.length) || 0
  return [
    { k: '监听地址', v: s.bind_ip || '127.0.0.1' },
    { k: '监听端口', v: `HTTP ${s.http_port || 24196} / HTTPS ${s.https_port || 25584}` },
    { k: 'hosts 主机数', v: String(s.host_count ?? '-') },
    { k: '上游域名', v: up ? `共 ${up} 个` : '-' },
    { k: '更新时间', v: s.timestamp || '' }
  ]
})

// 组件状态面板：按原版 10 项，映射到后端真实能力
const comps = computed(() => {
  const s = st.value
  const svcs = s.services || {}
  const P = (k, v, kind) => {
    kind = kind || (v === '运行中' ? 'on' : v === '已停止' || v === '失败' ? 'off' : 'na')
    return { k, v, kind }
  }
  return [
    P('证书', svcs.caddy === 'active' && (store.settings?.ca_years || 0) >= 1 ? '运行中' : '已停止'),
    P('ECH', '直连（不支持）'),
    P('Caddy', svcs.caddy ? unitCN(svcs.caddy) : '已停止'),
    P('端口转发', svcs.fwd ? unitCN(svcs.fwd) : '已停止'),
    P('Hosts', s.hosts_on ? '运行中' : '已停止'),
    P('DNS', s.dns_redirect ? '运行中' : '已停止'),
    P('系统代理', s.system_proxy && s.system_proxy !== '不处理' ? '运行中' : '关闭'),
    P('CDN', s.cdn_prefer ? `运行中 · ${s.cdn_pinned || 0} 条` : '已停止'),
    P('Twitch 掉宝', '直连（不支持）'),
    P('支持开发者', s.dev_support ? '运行中' : '已停止')
  ]
})
</script>

<template>
  <div class="svc-grid">
    <div class="mode-panel">
      <div class="mode-panel__hed">
        <h4>网络接管模式</h4>
        <span class="mode-panel__tip">
          <component :is="Globe" :size="12" /> 包含泛域名，部分访问需要 DNS 重定向模式 / 系统代理模式
        </span>
      </div>
      <div class="mode-panel__cards">
        <div v-for="m in modes" :key="m.key" class="mode-card">
          <component :is="m.icon" :size="16" class="mode-card__icon" />
          <div class="mode-card__body">
            <div class="mode-card__title">
              <span><component :is="Globe" :size="11" /> {{ m.title }}</span>
              <span class="mode-dot" :class="{ on: m.on }"></span>
            </div>
            <p class="mode-card__desc">{{ m.desc }}</p>
          </div>
          <button class="mode-card__go" @click="goSettings(m.sub)">详细设置 →</button>
        </div>
      </div>
    </div>

    <div class="col-left">
      <div class="toolbar">
        <div class="stat"><b>{{ enabled }}</b><span>/{{ total }} 规则启用</span></div>
        <div class="qbox">
          <Search :size="14" class="qi" />
          <input class="qinp" type="text" placeholder="搜索规则名称或说明" v-model="q" />
          <button v-if="q" class="qclr" @click="q = ''"><X :size="13" /></button>
        </div>
        <div class="ops">
          <button class="btn ghostb" @click="bulkAll(true)"><CheckSquare :size="14" /> 全部启用</button>
          <button class="btn ghostb" @click="bulkAll(false)"><Square :size="14" /> 全部停用</button>
          <button class="btn ghostb" @click="openBlacklist()"><ListPlus :size="14" /> 域名黑名单</button>
        </div>
      </div>

      <div v-if="store.rules.length === 0" class="empty">正在读取规则…</div>
      <div v-else-if="!secs.length" class="empty">没有匹配「{{ q }}」的规则</div>

      <section v-for="sec in secs" :key="sec.key" class="sec">
        <div class="sec-head">
          <h3>{{ sec.label }}</h3>
          <span class="cnt">{{ sec.items.filter((i) => i.rule.enabled).length }}/{{ sec.items.length }} 已启用</span>
        </div>
        <div class="fcards">
          <div
            v-for="it in sec.items"
            :key="it.rule.id"
            class="fcard"
            :class="{ off: !it.rule.enabled }"
          >
            <div class="ftxt">
              <div class="ft">{{ it.t }}</div>
              <div class="fd">{{ it.d }}</div>
              <button class="domlnk" @click="viewDomains(it.rule)"><Globe :size="12" /> 查看域名列表</button>
            </div>
            <button class="ibtn" title="编辑规则 JSON" @click="openEditor(it.rule.id)"><Pencil :size="14" /></button>
            <ToggleSwitch :model-value="it.rule.enabled" @change="(v) => toggleRule(it.rule, v)" />
          </div>
        </div>
      </section>
    </div>

    <aside class="col-right">
      <div class="ctl">
        <h4>服务控制</h4>
        <div class="ctl-ops">
          <button class="btn" @click="startAll()"><Power :size="14" /> 启动服务</button>
          <button class="btn sec" @click="stopAll()"><Power :size="14" /> 停止服务</button>
          <button class="btn ghostb" @click="regen()"><RefreshCw :size="14" /> 重载配置</button>
          <button class="btn ghostb" @click="refreshAll()"><RefreshCw :size="14" /> 刷新状态</button>
        </div>
      </div>

      <div class="vitals">
        <h4>网络监听 / 设置</h4>
        <div v-for="v in vitals" :key="v.k" class="vi">
          <span class="vk">{{ v.k }}</span>
          <span class="vv">{{ v.v }}</span>
        </div>
      </div>

      <div class="vitals">
        <h4>DNS 详情</h4>
        <template v-if="dnsDetail.length">
          <div v-for="d in dnsDetail" :key="d.k" class="vi">
            <span class="vk">{{ d.k }}</span>
            <span class="vv" :class="['运行中', '已接管', '已生效'].includes(d.v) ? 'on' : d.v === '已停止' || d.v === '未接管' || d.v === '未生效' ? 'off' : ''">{{ d.v }}</span>
          </div>
        </template>
        <div v-else class="dd-empty"><Network :size="14" /> DNS 快照不可用</div>
        <div class="vi"><span class="vk">Twitch 掉宝</span><span class="vv off">依赖 caddy 注入，需自行重编译</span></div>
      </div>

      <div class="vitals">
        <h4>组件状态</h4>
        <div v-for="c in comps" :key="c.k" class="vi">
          <span class="vk">{{ c.k }}</span>
          <span class="vv" :class="[c.kind]">{{ c.v }}</span>
        </div>
      </div>
    </aside>

    <EditorModal v-if="editor" :mode="editor.mode" :id="editor.id" @close="editor = null" @saved="reload" />

    <div v-if="domainsModal" class="mask" @click.self="domainsModal = null">
      <div class="frame">
        <div class="head">
          <span class="ti">域名列表 · {{ domainsModal.name }}</span>
          <button class="x" @click="domainsModal = null"><X :size="15" /></button>
        </div>
        <div class="dlist">
          <div v-for="(h, i) in domainsModal.hosts" :key="i" class="dline"><List :size="13" class="dico" />{{ h }}</div>
          <div v-if="!domainsModal.hosts.length" class="deof">该规则未包含具体域名（可能使用泛域名/通配规则）</div>
        </div>
        <div class="foot">
          <span class="note">共 {{ domainsModal.hosts.length }} 条域名</span>
          <button class="btn ghostb" @click="domainsModal = null">关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.svc-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 280px;
  gap: 18px;
  align-items: start;
}
/* —— 网络接管模式面板（对齐原版 mode-panel） —— */
.mode-panel {
  grid-column: 1 / -1;
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 8px;
  padding: 12px 14px 14px;
}
.mode-panel__hed {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.mode-panel__hed h4 {
  margin: 0;
  color: var(--color-strong);
  font-size: 14px;
  font-weight: 700;
}
.mode-panel__tip {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--color-faint);
  font-size: 11.5px;
}
.mode-panel__cards {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}
.mode-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  background: var(--color-bg);
  border: 1px solid var(--color-border);
  border-radius: 8px;
  padding: 12px;
}
.mode-card__icon {
  color: var(--color-primary-hi);
}
.mode-card__body {
  flex: 1;
}
.mode-card__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  color: var(--color-strong);
  font-size: 13px;
  font-weight: 700;
}
.mode-card__title > span:first-child {
  display: flex;
  align-items: center;
  gap: 5px;
}
.mode-card__desc {
  margin: 6px 0 0;
  color: var(--color-muted);
  font-size: 11.5px;
  line-height: 1.55;
}
.mode-card__go {
  align-self: flex-start;
  border: 1px solid var(--color-border);
  background: transparent;
  color: var(--color-muted);
  font-size: 12px;
  padding: 5px 10px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.12s;
}
.mode-card__go:hover {
  color: var(--color-primary-hi);
  border-color: var(--color-primary);
}
.mode-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-faint);
  display: inline-block;
}
.mode-dot.on {
  background: var(--color-on);
  box-shadow: 0 0 6px rgba(101, 183, 122, 0.6);
}
@media (max-width: 860px) {
  .mode-panel__cards {
    grid-template-columns: 1fr;
  }
}
.col-right {
  position: sticky;
  top: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}
.stat {
  font-size: 13px;
  color: var(--color-muted);
}
.stat b {
  color: var(--color-primary-hi);
  font-size: 18px;
  margin-right: 2px;
}
.qbox {
  display: flex;
  align-items: center;
  gap: 7px;
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  padding: 6px 10px;
  flex: 1;
  min-width: 200px;
}
.qi {
  color: var(--color-faint);
  flex: none;
}
.qinp {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: 0;
  outline: none;
  color: var(--color-fg);
  font-size: 12.5px;
}
.qclr {
  border: 0;
  background: transparent;
  color: var(--color-faint);
  cursor: pointer;
  display: inline-flex;
  padding: 2px;
}
.ops {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.sec {
  margin-bottom: 16px;
}
.sec-head {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 0 2px 8px;
}
.sec-head h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 700;
  color: var(--color-strong);
}
.sec-head .cnt {
  color: var(--color-faint);
  font-size: 12px;
}
.fcards {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.fcard {
  display: flex;
  align-items: center;
  gap: 12px;
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  padding: 10px 14px;
  transition: border-color 0.15s, background 0.15s;
}
.fcard:hover {
  border-color: var(--color-line-strong);
  background: var(--color-bg-soft);
}
.fcard.off {
  opacity: 0.72;
}
.fcard.off .ft {
  color: var(--color-faint);
}
.ftxt {
  flex: 1;
  min-width: 0;
}
.ft {
  color: var(--color-strong);
  font-size: 13.5px;
  font-weight: 600;
}
.fd {
  color: var(--color-muted);
  font-size: 12px;
  margin-top: 3px;
}
.domlnk {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border: 0;
  background: transparent;
  color: var(--color-faint);
  font-size: 11.5px;
  cursor: pointer;
  padding: 2px 0;
  margin-top: 4px;
}
.domlnk:hover {
  color: var(--color-primary-hi);
}
.ibtn {
  width: 26px;
  height: 26px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--color-muted);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.12s;
  flex: none;
}
.ibtn:hover {
  color: var(--color-strong);
  background: var(--color-primary-deep);
}
.ctl,
.vitals {
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  padding: 12px 14px;
}
.ctl h4,
.vitals h4 {
  margin: 0 0 10px;
  font-size: 12.5px;
  color: var(--color-muted);
  font-weight: 700;
  letter-spacing: 0.5px;
}
.ctl-ops {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.vi {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  padding: 5px 0;
  border-top: 1px dashed var(--color-border-soft);
  font-size: 12.5px;
}
.vi:first-of-type {
  border-top: 0;
}
.vk {
  color: var(--color-muted);
}
.vv {
  font-family: ui-monospace, Consolas, monospace;
  text-align: right;
  color: var(--color-fg);
}
.vv.on {
  color: var(--color-on);
}
.vv.off {
  color: var(--color-faint);
}
.vv.na {
  color: var(--color-muted);
}
.dd-empty {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 0;
  color: var(--color-faint);
  font-size: 12px;
}
.empty {
  text-align: center;
  color: var(--color-faint);
  font-size: 13px;
  padding: 30px;
  border: 1px dashed var(--color-border);
  border-radius: 6px;
}
.btn {
  border: 0;
  border-radius: 6px;
  background: linear-gradient(135deg, var(--color-primary-hi), var(--color-primary));
  color: var(--on-accent);
  font-weight: 600;
  font-size: 13px;
  padding: 8px 14px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  cursor: pointer;
  transition: all 0.12s;
}
.btn:hover {
  filter: brightness(1.1);
}
.btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.btn.sec {
  background: var(--color-primary-deep);
  color: var(--color-on-deep);
}
.btn.ghostb {
  background: transparent;
  border: 1px solid var(--color-border);
  color: var(--color-muted);
}
.btn.ghostb:hover {
  color: var(--color-strong);
  border-color: var(--color-primary);
  filter: none;
}
.mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 50;
}
.frame {
  width: min(560px, 92vw);
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  overflow: hidden;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--color-border);
}
.ti {
  font-weight: 700;
  color: var(--color-strong);
  font-size: 14px;
}
.x {
  border: 0;
  background: transparent;
  color: var(--color-muted);
  cursor: pointer;
  border-radius: 6px;
  padding: 4px;
  display: inline-flex;
}
.x:hover {
  color: var(--color-strong);
  background: var(--color-primary-deep);
}
.dlist {
  max-height: 340px;
  overflow: auto;
  padding: 8px 16px;
}
.dline {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 0;
  border-bottom: 1px dashed var(--color-border-soft);
  font: 12.5px/1.5 ui-monospace, Consolas, monospace;
  color: var(--color-fg);
}
.dico {
  color: var(--color-faint);
  flex: none;
}
.deof {
  padding: 24px;
  text-align: center;
  color: var(--color-faint);
  font-size: 12.5px;
}
.foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 11px 16px;
  border-top: 1px solid var(--color-border);
}
.note {
  font-size: 11.5px;
  color: var(--color-faint);
}
</style>