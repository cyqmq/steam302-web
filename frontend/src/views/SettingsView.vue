<script setup>
import { ref, computed, watch, inject } from 'vue'
import {
  Laptop, Play, RefreshCw, Minimize2, LogOut, Server, Network, Signal,
  Pencil, File, Copy, History, Globe, Zap, List, FileText, Shield, Earth,
  Gauge, Cloud, Lock, Calendar, Info, Heart, BookOpen, Palette, Settings,
  Trash2
} from 'lucide-vue-next'
import SettingCard from '../components/SettingCard.vue'
import SettingRow from '../components/SettingRow.vue'
import ToggleSwitch from '../components/ToggleSwitch.vue'
import CustomRadio from '../components/CustomRadio.vue'
import CustomCheckbox from '../components/CustomCheckbox.vue'
import CustomSelect from '../components/CustomSelect.vue'
import { put, post, get } from '../lib/api.js'
import { store, toast, setAccent } from '../lib/state.js'

const reload = inject('reload', async () => {})

const s = computed(() => store.settings || {})
const ast = computed(() => s.value.autostart_mode || 'disabled')
const hostsOn = computed(() => !!(store.status && store.status.hosts_on))

const keepSel = ref('100')
const logSel = ref('0')
const certYears = computed(() => String(s.value.ca_years || 10))
const freq = computed(() => s.value.dev_freq || 'weekly')
const inLog = computed(() => (s.value.log_max_bytes || 0) > 0)
const inBackup = computed(() => (s.value.backup_keep || 0) > 0)
const inDNS = computed(() => !!(store.status && store.status.dns_redirect))

const mbps = ref('20')
watch(
  () => s.value.prefer && s.value.prefer.max_mbps,
  (v) => (mbps.value = String(v || 20)),
  { immediate: true }
)

const freqOptions = [
  { value: 'weekly', label: '每周一次' },
  { value: 'daily', label: '每天一次' },
  { value: 'none', label: '不进行赞助' }
]
const keepOptions = [
  { value: '10', label: '10 份' },
  { value: '50', label: '50 份' },
  { value: '100', label: '100 份' },
  { value: '500', label: '500 份' }
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
const proxyModeOptions = [
  { value: 'stop', label: '停止监听 & 禁用代理' },
  { value: 'manual', label: '手动代理（127.0.0.1:28000）' }
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
    toast('测速速率限制已保存')
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

function nextAccent() {
  const order = ['red', 'blue', 'purple']
  const i = order.indexOf(store.accent)
  const next = order[(i + 1) % order.length]
  setAccent(next)
  toast('已切换主题：' + (next === 'red' ? '胭脂红' : next === 'blue' ? '冰晶蓝' : '丁香紫'))
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
    <!-- ① 启动行为 -->
    <SettingCard title="启动行为" :icon="Play">
      <template #actions><span class="noop"></span></template>
      <SettingRow
        title="开机自动运行"
        subtitle="后台服务 = 托管于 systemd；服务器无桌面环境时前台运行仅记录偏好"
        :icon="Laptop"
      >
        <div class="radios">
          <CustomRadio :model-value="ast" value="foreground" label="前台运行" @update:model-value="saveAst" />
          <CustomRadio :model-value="ast" value="service" label="后台服务(无界面)" @update:model-value="saveAst" />
          <CustomRadio :model-value="ast" value="disabled" label="禁用" @update:model-value="saveAst" />
        </div>
      </SettingRow>
      <SettingRow title="程序启动后自动开启服务" :icon="Play">
        <ToggleSwitch :model-value="!!s.start_service" @change="(v) => save({ start_service: v })" />
      </SettingRow>
      <SettingRow title="启动服务后自动更新配置" :icon="RefreshCw">
        <ToggleSwitch :model-value="!!s.auto_update" @change="(v) => save({ auto_update: v })" />
      </SettingRow>
      <SettingRow title="退出UI时同步退出后端服务" :icon="LogOut">
        <ToggleSwitch :model-value="!!s.exit_sync" @change="(v) => save({ exit_sync: v })" />
      </SettingRow>
      <SettingRow title="启动服务后自动最小化到托盘" :icon="Minimize2">
        <ToggleSwitch :model-value="!!s.minimize_tray" @change="(v) => save({ minimize_tray: v })" />
      </SettingRow>
    </SettingCard>

    <!-- ② 本地监听设置 -->
    <SettingCard title="本地监听设置" :icon="Server">
      <SettingRow
        title="本地监听 IP"
        subtitle="代理与转发服务绑定的回环地址"
        :icon="Network"
      >
        <input class="inp" type="text" spellcheck="false" :value="s.bind_ip || '127.0.0.1'" @change="(e) => save({ bind_ip: e.target.value.trim() }, '监听 IP 已保存')">
      </SettingRow>
      <SettingRow
        title="监听端口"
        subtitle="由 caddy 接管 80/443，与 302 原版一致"
        :icon="Signal"
      >
        <input class="inp" type="text" value="80 / 443" disabled>
      </SettingRow>
    </SettingCard>

    <!-- ③ hosts 模式设置 -->
    <SettingCard title="hosts 模式设置" :icon="Pencil">
      <SettingRow
        title="自动修改 Hosts"
        subtitle="为需要代理的域名写入 127.0.0.1 hosts 条目"
        :icon="File"
      >
        <ToggleSwitch :model-value="hostsOn" @change="toggleHosts" />
      </SettingRow>
      <SettingRow
        title="自动备份 Hosts"
        subtitle="每次修改前备份一次原文件"
        :icon="Copy"
      >
        <ToggleSwitch :model-value="inBackup" @change="setBackup" />
      </SettingRow>
      <SettingRow
        title="备份保留数量"
        :icon="History"
      >
        <CustomSelect
          :model-value="keepSel"
          :options="keepOptions"
          @change="(e) => { keepSel = e.value; setKeep(e.value) }"
        />
      </SettingRow>
    </SettingCard>

    <!-- ④ DNS 重定向模式 -->
    <SettingCard title="DNS重定向模式" :icon="Globe">
      <SettingRow
        title="启用DNS重定向"
        subtitle="开启后由 steam302-web-dnsd 接管 *.steamstatic.com 等的 DNS"
        :icon="Globe"
      >
        <ToggleSwitch :model-value="inDNS" @change="toggleDNS" />
      </SettingRow>
      <SettingRow
        title="DNS重定向CDN优选"
        subtitle="让 dnsd 优先返回优选后的 CDN 节点"
        :icon="Zap"
      >
        <ToggleSwitch :model-value="!!s.dns_cdn_prefer" @change="(v) => save({ dns_cdn_prefer: v })" />
      </SettingRow>
      <SettingRow
        title="用户自定义规则"
        subtitle="在 dnsd 默认规则基础上追加自定义映射"
        :icon="List"
      >
        <ToggleSwitch :model-value="!!s.dns_user_rules" @change="(v) => save({ dns_user_rules: v })" />
      </SettingRow>
      <SettingRow title="输出DNS重定向日志" :icon="FileText">
        <ToggleSwitch :model-value="!!s.dns_log" @change="(v) => save({ dns_log: v })" />
      </SettingRow>
    </SettingCard>

    <!-- ⑤ 系统代理模式 -->
    <SettingCard title="系统代理模式" :icon="Shield">
      <SettingRow
        title="自动修改代理(Windows)"
        subtitle="Web 版仅记录偏好，由桌面端执行"
        :icon="Earth"
      >
        <ToggleSwitch :model-value="!!s.auto_win_proxy" @change="(v) => save({ auto_win_proxy: v })" />
      </SettingRow>
      <SettingRow
        title="监听端口 & 代理模式"
        subtitle="Web 版不维护系统代理监听"
        :icon="Settings"
      >
        <CustomSelect :model-value="'stop'" :options="proxyModeOptions" disabled />
      </SettingRow>
      <SettingRow
        title="复制代理设置参数"
        subtitle="把代理端口/profile 信息复制到剪贴板使用"
        :icon="Copy"
      >
        <button class="btn ghostb" @click="loadProfile">复制代理设置</button>
      </SettingRow>
    </SettingCard>

    <!-- ⑥ CDN 优选 & 上游域名 -->
    <SettingCard title="CDN优选 & 上游域名" :icon="Cloud">
      <template #actions>
        <button class="ibtn circle" title="重新生成代理配置" @click="regen"><RefreshCw :size="15" /></button>
      </template>
      <SettingRow
        title="CDN优选"
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
        title="测速速率限制"
        :subtitle="'当前限速 ' + (s.prefer && s.prefer.max_mbps ? s.prefer.max_mbps : 20) + ' Mbps'"
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
        title="上游域名"
        subtitle="浏览图片等资源时使用的上游源"
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
        title="证书管理"
        subtitle="重新生成根证书或网站证书"
        :icon="Shield"
      >
        <button class="btn sec" @click="certReset('root')">重置根证书</button>
        <button class="btn sec" @click="certReset('leaf')">重置网站证书</button>
      </SettingRow>
    </SettingCard>

    <!-- ⑧ 支持 & 教程 & 主题 -->
    <SettingCard title="支持&教程&主题" :icon="Info">
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
            @change="(e) => { if (s.dev_support) save({ dev_freq: e.value }, '赞助频率已保存') }"
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
      <SettingRow
        title="界面主题"
        subtitle="胭脂红 ↔ 冰晶蓝 ↔ 丁香紫"
        :icon="Palette"
      >
        <button class="btn sec" @click="nextAccent"><Palette :size="15" /> 切换主题</button>
      </SettingRow>
    </SettingCard>

    <!-- ⑨ 高级选项 -->
    <SettingCard title="高级选项" :icon="Settings">
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
  background: #221d1e;
  border: 1px solid var(--color-border);
  color: #fff;
  border-radius: 8px;
  padding: 7px 11px;
  font-size: 13px;
  width: 210px;
  transition: all 0.15s;
}
.inp:hover:not(:disabled) {
  background: #262224;
  border-color: #4a4142;
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
.unitlabel {
  color: var(--color-muted);
  font-size: 12.5px;
}
.btn {
  border: 0;
  border-radius: 8px;
  background: linear-gradient(135deg, var(--color-primary-hi), var(--color-primary));
  color: #fff;
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
  color: #f0a9af;
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
  color: #fff;
  border-color: var(--color-primary);
  filter: none;
}
.btn.warn .wi {
  color: #ffd45e;
}
.ibtn.circle {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: 0;
  background: var(--color-primary);
  color: #fff;
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