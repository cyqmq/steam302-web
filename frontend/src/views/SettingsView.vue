<script setup>
import { ref, computed, watch, inject, onMounted } from 'vue'
import {
  Laptop, Play, RefreshCw, Minimize2, LogOut, Server, Network, Signal,
  Pencil, File, Copy, History, Globe, Zap, List, Shield, Gauge,
  Cloud, Lock, Calendar, Info, Heart, BookOpen, Settings,
  Trash2, Download, Clock
} from 'lucide-vue-next'
import SettingCard from '../components/SettingCard.vue'
import SettingRow from '../components/SettingRow.vue'
import ToggleSwitch from '../components/ToggleSwitch.vue'
import CustomRadio from '../components/CustomRadio.vue'
import CustomCheckbox from '../components/CustomCheckbox.vue'
import CustomSelect from '../components/CustomSelect.vue'
import { put, post, get } from '../lib/api.js'
import { store, toast } from '../lib/state.js'

const reload = inject('reload', async () => {})
const reloadStatus = inject('reloadStatus', async () => {})

const s = computed(() => store.settings || {})
const ast = computed(() => s.value.autostart_mode || 'disabled')
const hostsOn = computed(() => !!(store.status && store.status.hosts_on))

// —— 本地监听端口（可编辑，重载后生效）——
const httpsPort = ref('25584')
const httpPort = ref('24196')
watch(
  () => [s.value.https_port, s.value.http_port],
  ([h, p]) => {
    if (h) httpsPort.value = String(h)
    if (p) httpPort.value = String(p)
  },
  { immediate: true }
)

// —— DNS 能力组 ——
const dnsListen = ref('127.0.0.1:53')
const dnsUpstreamTxt = ref('')
const dnsTTL = ref('600')
const dnsAnswerIP = ref('127.0.0.1')
watch(
  () => [s.value.dns_listen, s.value.dns_upstream, s.value.dns_ttl, s.value.dns_answer_ip],
  ([l, u, t, a]) => {
    if (l) dnsListen.value = l
    if (Array.isArray(u)) dnsUpstreamTxt.value = u.join(', ')
    if (t) dnsTTL.value = String(t)
    if (a) dnsAnswerIP.value = a
  },
  { immediate: true }
)
const dnsResolv = computed(() => !!(s.value.dns_resolv_managed || (store.dns && store.dns.resolv_managed)))
const dnsLAN = computed(() => !!(s.value.dns_lan_redirect || (store.dns && store.dns.lan_redirect)))
const dnsActive = computed(() => !!((store.dns && store.dns.active) || (store.status && store.status.dns_redirect)))

// —— CDN 定时健康检测 ——
const preferTimer = ref({ enabled: false, minutes: 30 })
const preferMinutes = computed(() => String(preferTimer.value.minutes || 30))
const preferMinOptions = [15, 30, 60, 120, 240, 480].map((m) => ({ value: String(m), label: '每 ' + m + ' 分钟' }))
async function loadPreferTimer() {
  try {
    const d = await get('/api/prefer/timer')
    if (d) preferTimer.value = { enabled: !!d.enabled, minutes: d.minutes || 30 }
  } catch {
    preferTimer.value = { enabled: false, minutes: 30 }
  }
}
async function setPreferTimer(v) {
  try {
    const d = await post('/api/prefer/timer', { enabled: v, minutes: Number(preferMinutes.value) })
    if (d && d.ok) {
      preferTimer.value = { enabled: v, minutes: d.status?.minutes || Number(preferMinutes.value) }
      toast(v ? '定时健康检测已开启' : '定时健康检测已关闭')
    } else {
      toast((d && d.error) || '操作失败', 'err')
    }
  } catch (e) {
    toast('操作失败: ' + e.message, 'err')
  }
}
async function savePreferMinutes() {
  if (!preferTimer.value.enabled) return
  await setPreferTimer(true)
}

onMounted(loadPreferTimer)

const keepSel = ref('100')
const logSel = ref('0')
const certYears = computed(() => String(s.value.ca_years || 10))
const freq = computed(() => s.value.dev_freq || 'weekly')
const inLog = computed(() => (s.value.log_max_bytes || 0) > 0)
const inBackup = computed(() => (s.value.backup_keep || 0) > 0)

const mbps = ref('20')
watch(
  () => s.value.prefer && s.value.prefer.max_mbps,
  (v) => (mbps.value = String(v || 20)),
  { immediate: true }
)

const freqOptions = [
  { value: 'weekly', label: '每周' },
  { value: 'daily', label: '每天' },
  { value: 'none', label: '不赞助' }
]
const keepOptions = [
  { value: '10', label: '10 份' },
  { value: '30', label: '30 份' },
  { value: '50', label: '50 份' },
  { value: '100', label: '100 份' }
]
const logOptions = [
  { value: '0', label: '不限制' },
  { value: '5', label: '5 MB' },
  { value: '10', label: '10 MB' },
  { value: '50', label: '50 MB' }
]
const certOptions = [
  { value: '10', label: '有效期: 10年' },
  { value: '5', label: '有效期: 5年' },
  { value: '3', label: '有效期: 3年' },
  { value: '1', label: '有效期: 1年' }
]
const upsOptions = [
  { value: 'edge', label: 'Edge浏览器图片' },
  { value: 'mod', label: '吧主图片源' },
  { value: 'direct', label: '直连（不代理图片）' }
]

const cdnMap = ref({ akamai: false, cloudfront: false, cloudflare: false, fastly: false })
watch(
  () => store.rules,
  () => {
    const on = (id) => {
      const r = store.rules.find((x) => x.id === id)
      return !!(r && r.enabled)
    }
    cdnMap.value = {
      akamai: on('steam_cdn_akamai'),
      cloudfront: on('steam_cdn_cloudfront'),
      cloudflare: on('steam_cdn_cloudflare'),
      fastly: on('steam_cdn_fastly')
    }
  },
  { immediate: true, deep: true }
)

async function save(partial, tip = '已保存') {
  try {
    await put('/api/settings', partial)
    store.settings = { ...s.value, ...partial }
    toast(tip)
  } catch (e) {
    toast('保存失败: ' + e.message, 'err')
  }
}

function portNum(x, def) {
  const n = parseInt(x, 10)
  return Number.isFinite(n) && n > 0 && n < 65536 ? n : def
}
function savePorts() {
  const h = portNum(httpsPort.value, 25584)
  const p = portNum(httpPort.value, 24196)
  if (h === p) {
    toast('HTTPS 与 HTTP 端口不能相同', 'err')
    return
  }
  httpsPort.value = String(h)
  httpPort.value = String(p)
  save({ https_port: h, http_port: p }, '端口已保存，点击下方“重载服务”生效')
}
async function reloadServices() {
  if (portNum(httpsPort.value, 25584) !== (s.value.https_port || 25584) || portNum(httpPort.value, 24196) !== (s.value.http_port || 24196)) {
    const h = portNum(httpsPort.value, 25584)
    const p = portNum(httpPort.value, 24196)
    if (h === p) {
      toast('HTTPS 与 HTTP 端口不能相同', 'err')
      return
    }
    try {
      await put('/api/settings', { https_port: h, http_port: p })
      store.settings = { ...s.value, https_port: h, http_port: p }
    } catch (e) {
      toast('保存端口失败: ' + e.message, 'err')
      return
    }
  }
  try {
    await post('/api/services/reload', {})
    toast('配置已重新生成，服务已重载')
    reload()
  } catch (e) {
    toast('重载失败: ' + e.message, 'err')
  }
}

function saveDNSPort(v) {
  save({ dns_listen: v.trim() }, 'DNS 监听地址已保存')
}
function saveDNSUpstream() {
  const parts = dnsUpstreamTxt.value
    .split(/[,，\s]+/)
    .map((x) => x.trim())
    .filter(Boolean)
  if (!parts.length) {
    toast('请输入至少一个上游 DNS', 'err')
    return
  }
  const normalized = parts.map((p) => {
    if (/^\d+$/.test(p)) return p + ':53'
    if (!/:\d+$/.test(p)) return p + ':53'
    return p
  })
  dnsUpstreamTxt.value = normalized.join(', ')
  save({ dns_upstream: normalized }, '上游 DNS 已保存')
}
function saveDNSTTL(v) {
  const n = parseInt(v, 10)
  if (!Number.isFinite(n) || n < 1) {
    dnsTTL.value = String(s.value.dns_ttl || 600)
    toast('TTL 须为不小于 1 的整数', 'err')
    return
  }
  dnsTTL.value = String(n)
  save({ dns_ttl: n }, 'DNS TTL 已保存')
}
function saveDNSAnswer(v) {
  save({ dns_answer_ip: v.trim() }, 'DNS 应答 IP 已保存')
}

async function toggleResolv(v) {
  try {
    const d = await post('/api/dns/resolv', { enabled: v })
    if (d && d.ok) {
      store.settings = { ...s.value, dns_resolv_managed: v }
      toast(d.output ? v ? '已接管系统解析器（/etc/resolv.conf）' : '已释放系统解析器' : 'DNS 解析器设置已保存')
    } else {
      toast((d && d.output) || (d && d.error) || '操作失败', 'err')
    }
    reload()
  } catch (e) {
    toast('操作失败: ' + e.message, 'err')
  }
}
async function toggleLAN(v) {
  try {
    const d = await post('/api/dns/lan', { enabled: v })
    if (d && d.ok) {
      store.settings = { ...s.value, dns_lan_redirect: v }
      toast(v ? '局域网 53 端口重定向已生效（需已启动 DNS 服务）' : '局域网 53 端口重定向已移除')
    } else {
      toast((d && d.output) || (d && d.error) || '操作失败', 'err')
    }
    reloadFromDNS()
    reload()
  } catch (e) {
    toast('操作失败: ' + e.message, 'err')
  }
}
async function reloadFromDNS() {
  try {
    store.dns = await get('/api/dns')
  } catch {
    store.dns = null
  }
}
onMounted(reloadFromDNS)

function saveAst(v) {
  if (v === ast.value) return
  save({ autostart_mode: v }, '开机自启设置已保存')
  if (v !== 'foreground' && s.value.autostart) {
    post('/api/settings/autostart', { systemd: true }).catch(() => {})
  }
}

async function toggleHosts(v) {
  try {
    await post('/api/hosts', { enabled: v })
    toast(v ? '已写入 hosts，正在应用' : '已移除 hosts 条目')
    reload()
  } catch (e) {
    toast('修改 hosts 失败: ' + e.message, 'err')
  }
}

async function toggleDNS(v) {
  try {
    await post('/api/services/dns', { enabled: v })
    toast(v ? 'DNS 重定向已启动' : 'DNS 重定向已停止')
    reload()
  } catch (e) {
    toast('DNS 服务操作失败: ' + e.message, 'err')
  }
}

function setBackup(v) {
  const keep = v ? Number(keepSel.value) : 0
  save({ backup_keep: keep }, v ? '自动备份已开启' : '自动备份已关闭')
}
function setKeep(v) {
  if (!inBackup.value) return
  save({ backup_keep: Number(v) }, '备份保留设置已保存')
}

function setLog(v) {
  const mb = v ? Number(logSel.value) : 0
  save({ log_max_bytes: mb * 1024 * 1024 }, v ? '日志自动清除已开启' : '日志自动清除已关闭')
}
function setLogSel(v) {
  if (!inLog.value) return
  save({ log_max_bytes: Number(v) * 1024 * 1024 }, '日志保留上限已保存')
}

async function cdnSet(key, v) {
  if (key === 'cloudfront') {
    toast('当前版本无 Cloudfront 规则，可改选其余 CDN', 'warn')
    return
  }
  try {
    await post('/api/rules/steam_cdn_' + key, { enabled: v })
    toast('CDN 优选已' + (v ? '启用' : '关闭'))
    reload()
  } catch (e) {
    toast('操作失败: ' + e.message, 'err')
  }
}

async function saveMbps() {
  const v = Number(mbps.value)
  if (!Number.isFinite(v) || v < 0) {
    toast('请输入有效的限速值', 'err')
    return
  }
  const p = { ...(s.value.prefer || {}), max_mbps: v }
  try {
    await put('/api/settings', { prefer: p })
    store.settings = { ...s.value, prefer: p }
    toast('限速设置已保存')
  } catch (e) {
    toast('保存失败: ' + e.message, 'err')
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

async function certReset(what) {
  try {
    const d = await post('/api/cert/reset', { kind: what })
    toast(d && d.ok ? '证书已重置，需重新应用配置' : '证书重置完成')
  } catch (e) {
    toast('证书重置失败: ' + e.message, 'err')
  }
}

async function factoryReset() {
  if (!confirm('确定要恢复所有设置为出厂默认吗？（规则与黑名单不会删除）')) return
  try {
    await post('/api/settings/reset/ready', {})
    toast('已重置设置')
    reload()
  } catch (e) {
    toast('重置失败: ' + e.message, 'err')
  }
}

async function loadProfile() {
  try {
    const p = await get('/api/profile')
    await navigator.clipboard.writeText(JSON.stringify(p, null, 2))
    toast('代理参数已复制到剪贴板')
  } catch (e) {
    toast('复制失败: ' + e.message, 'err')
  }
}

const freqVal = computed(() => {
  const o = freqOptions.find((x) => x.value === freq.value)
  return o ? o.value : 'weekly'
})

const openTutorial = () => window.open('https://github.com/cyqmq/steam302-web', '_blank')
</script>

<template>
  <div class="sett">
    <!-- ① 启动与窗口 -->
    <SettingCard title="启动与窗口" :icon="Play">
      <template #actions><span class="noop"></span></template>
      <SettingRow
        title="开机后自动启动后台服务（后端）"
        subtitle="由 systemd 托管；服务器无桌面环境时前台运行仅记录偏好"
        :icon="Laptop"
      >
        <div class="radios">
          <CustomRadio :model-value="ast" value="foreground" label="前台运行" @update:model-value="saveAst" />
          <CustomRadio :model-value="ast" value="service" label="后台服务(无界面)" @update:model-value="saveAst" />
          <CustomRadio :model-value="ast" value="disabled" label="禁用" @update:model-value="saveAst" />
        </div>
      </SettingRow>
      <SettingRow title="打开程序后自动启动服务" :icon="Play">
        <ToggleSwitch :model-value="!!s.start_service" @change="(v) => save({ start_service: v })" />
      </SettingRow>
      <SettingRow title="启动服务后自动更新配置" :icon="RefreshCw">
        <ToggleSwitch :model-value="!!s.auto_update" @change="(v) => save({ auto_update: v })" />
      </SettingRow>
      <SettingRow title="退出UI时同步退出后端服务" :icon="LogOut">
        <ToggleSwitch :model-value="!!s.exit_sync" @change="(v) => save({ exit_sync: v })" />
      </SettingRow>
      <SettingRow
        title="启动服务后自动隐藏窗口"
        subtitle="服务启动成功后隐藏主窗口"
        :icon="Minimize2"
      >
        <ToggleSwitch :model-value="!!s.minimize_tray" @change="(v) => save({ minimize_tray: v })" />
      </SettingRow>
    </SettingCard>

    <!-- ② 本地监听设置 -->
    <SettingCard title="本地监听设置" :icon="Server">
      <SettingRow
        title="监听地址"
        subtitle="代理与转发服务绑定的回环地址"
        :icon="Network"
      >
        <input class="inp" type="text" spellcheck="false" :value="s.bind_ip || '127.0.0.1'" @change="(e) => save({ bind_ip: e.target.value.trim() }, '监听地址已保存')">
      </SettingRow>
      <SettingRow
        title="HTTPS 监听端口"
        subtitle="caddy 接管 HTTPS（默认 25584），修改后需重载服务"
        :icon="Signal"
      >
        <input class="inp" type="number" min="1" max="65535" v-model="httpsPort" @keyup.enter="savePorts" @blur="savePorts" />
      </SettingRow>
      <SettingRow
        title="HTTP 监听端口"
        subtitle="caddy 接管 HTTP（默认 24196），修改后需重载服务"
        :icon="Signal"
      >
        <input class="inp" type="number" min="1" max="65535" v-model="httpPort" @keyup.enter="savePorts" @blur="savePorts" />
      </SettingRow>
      <SettingRow
        title="重载代理服务"
        subtitle="重新生成配置并使端口 / 规则 / 上游域名变更生效"
        :icon="RefreshCw"
      >
        <button class="btn sec" @click="reloadServices"><RefreshCw :size="15" /> 重载服务</button>
      </SettingRow>
    </SettingCard>

    <!-- ③ hosts 模式设置 -->
    <SettingCard title="hosts 模式设置" :icon="Pencil">
      <SettingRow
        title="自动修改"
        subtitle="为需要代理的域名写入 127.0.0.1 hosts 条目"
        :icon="File"
      >
        <ToggleSwitch :model-value="hostsOn" @change="toggleHosts" />
      </SettingRow>
      <SettingRow
        title="自动备份"
        subtitle="每次修改前备份一次原文件"
        :icon="Copy"
      >
        <ToggleSwitch :model-value="inBackup" @change="setBackup" />
      </SettingRow>
      <SettingRow
        title="备份保留数量"
        subtitle="1–100 份"
        :icon="History"
      >
        <CustomSelect
          :model-value="keepSel"
          :options="keepOptions"
          @change="(e) => { keepSel = e.value; setKeep(e.value) }"
        />
      </SettingRow>
    </SettingCard>

    <!-- ④ DNS 能力组 -->
    <SettingCard title="DNS 能力组" :icon="Globe">
      <SettingRow
        title="启用本地 DNS 服务"
        subtitle="由 steam302-web-dnsd 接管对游戏/CDN 域名的解析请求，以支持自定义解析与完整伪 SNI"
        :icon="Globe"
      >
        <ToggleSwitch :model-value="dnsActive" @change="toggleDNS" />
      </SettingRow>
      <SettingRow
        title="监听地址"
        subtitle="DNS 服务监听地址，默认 127.0.0.1:53"
        :icon="Network"
      >
        <input class="inp" type="text" spellcheck="false" :value="dnsListen" @change="(e) => (dnsListen = e.target.value, saveDNSPort(e.target.value))">
      </SettingRow>
      <SettingRow
        title="上游 DNS"
        subtitle="逗号分隔，可带端口；将未拦截域名递归转发给这些解析器"
        :icon="Globe"
      >
        <input class="inp wide" type="text" spellcheck="false" v-model="dnsUpstreamTxt" @keyup.enter="saveDNSUpstream" @blur="saveDNSUpstream">
      </SettingRow>
      <SettingRow
        title="TTL"
        subtitle="域名结果生效时长（秒）"
        :icon="Clock"
      >
        <input class="inp" type="number" min="1" v-model="dnsTTL" @keyup.enter="saveDNSTTL" @blur="saveDNSTTL">
      </SettingRow>
      <SettingRow
        title="应答 IP"
        subtitle="被劫持的域名应答给本机/局域网设备的 IP（默认 127.0.0.1）"
        :icon="Globe"
      >
        <input class="inp" type="text" spellcheck="false" :value="dnsAnswerIP" @change="(e) => (dnsAnswerIP = e.target.value, saveDNSAnswer(e.target.value))">
      </SettingRow>
      <SettingRow
        title="查询日志"
        subtitle="记录 DNS 查询到 config/dnsd_queries.log，便于排查"
        :icon="List"
      >
        <ToggleSwitch :model-value="!!s.dns_query_log" @change="(v) => save({ dns_query_log: v }, v ? 'DNS 查询日志已开启' : 'DNS 查询日志已关闭')" />
      </SettingRow>
      <SettingRow
        title="系统解析器接管"
        subtitle="把 /etc/resolv.conf 指向本地 DNS 服务（dnsredir 管理，含快照还原）"
        :icon="File"
      >
        <ToggleSwitch :model-value="dnsResolv" @change="toggleResolv" />
      </SettingRow>
      <SettingRow
        title="局域网 53 端口重定向"
        subtitle="将局域网内设备发往 53 端口的 DNS 请求重定向到本地 DNS（iptables）"
        :icon="Network"
      >
        <ToggleSwitch :model-value="dnsLAN" @change="toggleLAN" />
      </SettingRow>
    </SettingCard>

    <!-- ⑥ CDN 优选 & 上游域名 -->
    <SettingCard title="CDN 优选 & 上游域名" :icon="Cloud">
      <template #actions>
        <button class="ibtn circle" title="重新生成代理配置" @click="regen"><RefreshCw :size="15" /></button>
      </template>
      <SettingRow
        title="CDN 优选"
        subtitle="勾选参与测速优选的 CDN 边缘"
        :icon="Zap"
      >
        <div class="chkrow">
          <CustomCheckbox label="Akamai" :model-value="cdnMap.akamai" @change="(v) => cdnSet('akamai', v)" />
          <CustomCheckbox label="Cloudfront" :model-value="cdnMap.cloudfront" @change="(v) => cdnSet('cloudfront', v)" />
          <CustomCheckbox label="Cloudflare" :model-value="cdnMap.cloudflare" @change="(v) => cdnSet('cloudflare', v)" />
          <CustomCheckbox label="Fastly" :model-value="cdnMap.fastly" @change="(v) => cdnSet('fastly', v)" />
        </div>
      </SettingRow>
      <SettingRow
        title="手动与启动测速总限速"
        :subtitle="'所有并发下载共享此带宽上限（当前 ' + (s.prefer && s.prefer.max_mbps ? s.prefer.max_mbps : 20) + ' Mbps）'"
        :icon="Gauge"
      >
        <input
          class="inp unit"
          type="number"
          min="0"
          step="10"
          v-model="mbps"
          @keyup.enter="saveMbps"
          @blur="saveMbps"
        >
        <span class="unitlabel">Mbps</span>
      </SettingRow>
      <SettingRow
        title="定时健康检测"
        :subtitle="preferTimer.enabled ? 'systemd timer 每 ' + preferMinutes + ' 分钟自动测速并更新优选边缘' : '定期自动测速，替换失速的边缘，保持 CDN 优选新鲜'"
        :icon="Clock"
      >
        <div class="pair">
          <ToggleSwitch :model-value="preferTimer.enabled" @change="setPreferTimer" />
          <CustomSelect
            :model-value="preferMinutes"
            :options="preferMinOptions"
            :disabled="!preferTimer.enabled"
            @change="(e) => { preferTimer.minutes = Number(e.value); savePreferMinutes() }"
          />
        </div>
      </SettingRow>
      <SettingRow
        title="上游域名 (Steam相关)"
        subtitle="用于避开部分运营商访问干扰"
        :icon="Globe"
      >
        <CustomSelect :model-value="'edge'" :options="upsOptions" disabled />
      </SettingRow>
    </SettingCard>

    <!-- ⑦ 安全证书 -->
    <SettingCard title="安全证书" :icon="Lock">
      <SettingRow
        title="证书有效期"
        subtitle="HTTPS 网站证书的过期周期"
        :icon="Calendar"
      >
        <CustomSelect
          :model-value="certYears"
          :options="certOptions"
          @change="(e) => { save({ ca_years: Number(e.value) }, '证书有效期已保存') }"
        />
      </SettingRow>
      <SettingRow
        title="证书重置"
        subtitle="重新生成根证书或网站证书"
        :icon="Shield"
      >
        <button class="btn sec" @click="certReset('root')">重置根证书</button>
        <button class="btn sec" @click="certReset('leaf')">重置网站证书</button>
      </SettingRow>
    </SettingCard>

    <!-- ⑧ 支持 & 教程 -->
    <SettingCard title="支持 & 教程" :icon="Info">
      <SettingRow
        title="支持开发者"
        subtitle="在应用内展示一条赞助入口"
        :icon="Heart"
      >
        <div class="pair">
          <ToggleSwitch :model-value="!!s.dev_support" @change="(v) => save({ dev_support: v })" />
          <CustomSelect
            :model-value="freqVal"
            :options="freqOptions"
            :disabled="!s.dev_support"
            @change="(e) => { if (s.dev_support) save({ dev_freq: e.value }, '支持周期已保存') }"
          />
        </div>
      </SettingRow>
      <SettingRow
        title="使用教程"
        subtitle="打开项目主页查看使用说明"
        :icon="BookOpen"
      >
        <button class="btn" @click="openTutorial"><BookOpen :size="15" /> 使用教程</button>
      </SettingRow>
    </SettingCard>

    <!-- ⑨ 高级选项 -->
    <SettingCard title="高级选项" :icon="Settings">
      <SettingRow
        title="复制代理设置参数"
        subtitle="把代理端口/Profile 信息复制到剪贴板供 PC 客户端使用"
        :icon="Copy"
      >
        <button class="btn ghostb" @click="loadProfile">复制代理设置</button>
      </SettingRow>
      <SettingRow
        title="下载 PAC 文件"
        subtitle="浏览器/系统代理可导入 proxy.pac 按规则自动分流"
        :icon="Download"
      >
        <a class="btn ghostb" href="/proxy.pac" download="proxy.pac"><Download :size="15" /> 下载 PAC</a>
      </SettingRow>
      <SettingRow
        title="日志自动清除"
        subtitle="后端日志达到上限后自动轮转"
        :icon="List"
      >
        <div class="pair">
          <ToggleSwitch :model-value="inLog" @change="setLog" />
          <CustomSelect
            :model-value="logSel"
            :options="logOptions"
            :disabled="!inLog"
            @change="(e) => { logSel = e.value; setLogSel(e.value) }"
          />
        </div>
      </SettingRow>
      <SettingRow
        title="重置所有设置"
        subtitle="恢复程序默认设置，需要重新应用配置"
        :icon="Trash2"
      >
        <button class="btn warn" @click="factoryReset"><Trash2 :size="15" class="wi" /> 重置所有设置</button>
      </SettingRow>
    </SettingCard>
  </div>
</template>

<style scoped>
.sett {
  max-width: 900px;
  margin: 0 auto;
}
.radios {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.pair {
  display: flex;
  align-items: center;
  gap: 9px;
}
.chkrow {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.inp {
  background: var(--color-card);
  border: 1px solid var(--color-border);
  color: var(--color-fg);
  border-radius: 6px;
  padding: 7px 11px;
  font-size: 13px;
  width: 210px;
  transition: all 0.15s;
}
.inp:hover:not(:disabled) {
  background: var(--color-hover);
  border-color: var(--color-line-strong);
}
.inp:focus {
  outline: none;
  border-color: var(--color-primary);
}
.inp:disabled {
  color: var(--color-faint);
  cursor: not-allowed;
  text-align: center;
}
.inp.unit {
  width: 110px;
  text-align: right;
}
.inp.wide {
  width: 320px;
}
.unitlabel {
  color: var(--color-muted);
  font-size: 12.5px;
}
.btn {
  border: 0;
  border-radius: 6px;
  background: linear-gradient(135deg, var(--color-primary-hi), var(--color-primary));
  color: var(--on-accent);
  font-weight: 600;
  font-size: 13px;
  padding: 8px 16px;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  cursor: pointer;
  transition: all 0.12s;
}
.btn:hover {
  filter: brightness(1.1);
}
.btn.sec {
  background: var(--color-primary-deep);
  color: var(--color-on-deep);
}
.btn.sec:hover {
  filter: brightness(1.25);
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
.btn.warn .wi {
  color: var(--color-warn);
}
.ibtn.circle {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: 0;
  background: var(--color-primary);
  color: var(--on-accent);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.12s;
}
.ibtn.circle:hover {
  filter: brightness(1.15);
}
</style>