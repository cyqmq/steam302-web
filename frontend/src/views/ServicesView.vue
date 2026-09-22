<script setup>
import { ref, computed, inject } from 'vue'
import { Pencil, ListPlus, CheckSquare, Square, RefreshCw, Power } from 'lucide-vue-next'
import ToggleSwitch from '../components/ToggleSwitch.vue'
import EditorModal from '../components/EditorModal.vue'
import { post } from '../lib/api.js'
import { store, toast } from '../lib/state.js'

const reload = inject('reload', async () => {})

// 临时功能卡映射：id → { g(分组), t(标题), d(说明) }，后续按实际支持范围再调整
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
  hb_fanatical_imgfix: { g: 'other', t: 'HB / Fanatical 图片修复', d: '修复 Humble Bundle / Fanatical 商店图片加载。' }
}

const SECT = [
  { key: 'steam', label: 'Steam 卡片' },
  { key: 'ea', label: 'EA 卡片' },
  { key: 'other', label: '其他服务' }
]

const secs = computed(() =>
  SECT.map((sec) => {
    const items = store.rules.map((r) => {
      const m = FEAT[r.id] || {}
      return {
        rule: r,
        g: m.g || 'other',
        t: m.t || r.name || r.id,
        d: m.d || r.description || ''
      }
    }).filter((x) => x.g === sec.key)
    return { ...sec, items }
  }).filter((s) => s.items.length)
)

const total = computed(() => store.rules.length)
const enabled = computed(() => store.rules.filter((r) => r.enabled).length)

const st = computed(() => store.status || {})

const editor = ref(null)
const openEditor = (id) => (editor.value = { mode: 'rule', id })
const openBlacklist = () => (editor.value = { mode: 'blacklist' })

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

async function applyAll() {
  try {
    const d = await post('/api/apply', {})
    if (d && d.applied) {
      toast('已重新生成并应用配置')
    } else {
      toast((d && (d.error || d.hint)) || '应用失败', 'err')
    }
    reload()
  } catch (e) {
    toast('应用失败: ' + e.message, 'err')
  }
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

const vitals = computed(() => {
  const s = st.value
  const up = (s.upstream && s.upstream.length) || 0
  const svcs = s.services || {}
  const svc = (name) => ({ v: unitCN(svcs[name]), on: svcs[name] === 'active' })
  return [
    { k: '本地监听端口', v: `${s.https_port || 25584} / ${s.http_port || 24196}` },
    { k: '绑定 IP', v: s.bind_ip || '127.0.0.1' },
    { k: 'hosts 主机数', v: String(s.host_count ?? '-') },
    { k: 'CDN 优选', v: s.cdn_prefer ? `开启 · ${s.cdn_pinned || 0} 条` : '关闭' },
    { k: '上游域名', v: up ? `共 ${up} 个` : '-' },
    { k: 'DNS 重定向', v: s.dns_redirect ? '开启' : '关闭' },
    { k: '系统代理', v: s.system_proxy || '不处理' },
    { k: 'caddy', ...svc('caddy') },
    { k: 'fwd', ...svc('fwd') },
    { k: 'dnsd', ...svc('dnsd') },
    { k: 'webui', ...svc('webui') },
    { k: '更新时间', v: s.timestamp || '' }
  ]
})
</script>

<template>
  <div class="svc-grid">
    <div class="col-left">
      <div class="toolbar">
        <div class="stat"><b>{{ enabled }}</b><span>/{{ total }} 规则已启用</span></div>
        <div class="ops">
          <button class="btn ghostb" @click="bulkAll(true)"><CheckSquare :size="14" /> 全部启用</button>
          <button class="btn ghostb" @click="bulkAll(false)"><Square :size="14" /> 全部停用</button>
          <button class="btn ghostb" @click="openBlacklist()"><ListPlus :size="14" /> 域名黑名单</button>
        </div>
      </div>

      <div v-if="store.rules.length === 0" class="empty">正在读取规则…</div>

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
          <button class="btn" :disabled="!st.services || st.services.fwd !== 'active'" @click="applyAll()">
            <RefreshCw :size="14" /> 重载配置
          </button>
          <button class="btn sec" @click="stopAll()"><Power :size="14" /> 停止全部</button>
        </div>
      </div>

      <div class="vitals">
        <h4>网络监听 / 设置</h4>
        <div v-for="v in vitals" :key="v.k" class="vi">
          <span class="vk">{{ v.k }}</span>
          <span class="vv" :class="{ on: v.on }">{{ v.v }}</span>
        </div>
      </div>
    </aside>

    <EditorModal v-if="editor" :mode="editor.mode" :id="editor.id" @close="editor = null" @saved="reload" />
  </div>
</template>

<style scoped>
.svc-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 280px;
  gap: 18px;
  align-items: start;
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
  color: #fff;
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
  border-radius: 8px;
  padding: 10px 14px;
  transition: border-color 0.15s, background 0.15s;
}
.fcard:hover {
  border-color: #4a4142;
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
  color: #fff;
  font-size: 13.5px;
  font-weight: 600;
}
.fd {
  color: var(--color-muted);
  font-size: 12px;
  margin-top: 3px;
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
  color: #fff;
  background: var(--color-primary-deep);
}
.ctl,
.vitals {
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 8px;
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
.btn {
  border: 0;
  border-radius: 6px;
  background: linear-gradient(135deg, var(--color-primary-hi), var(--color-primary));
  color: #fff;
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
  color: #f0a9af;
}
.btn.ghostb {
  background: transparent;
  border: 1px solid var(--color-border);
  color: var(--color-muted);
}
.btn.ghostb:hover {
  color: #fff;
  border-color: var(--color-primary);
  filter: none;
}
</style>