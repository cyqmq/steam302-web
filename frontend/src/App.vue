<script setup>
import { ref, shallowRef, provide, markRaw, onMounted, computed } from 'vue'
import { Server, Settings, Activity, ScrollText, Info, Power } from 'lucide-vue-next'
import ServicesView from './views/ServicesView.vue'
import SettingsView from './views/SettingsView.vue'
import ConnectionsView from './views/ConnectionsView.vue'
import LogsView from './views/LogsView.vue'
import AboutView from './views/AboutView.vue'
import { reloadAll, loadStatus } from './lib/data.js'
import { store, setTheme } from './lib/state.js'
import logoUrl from './assets/logo.png'

const tabs = [
  { key: 'services', label: '服务', icon: Server, comp: markRaw(ServicesView), sub: '开关代理规则 · 服务控制' },
  { key: 'settings', label: '设置', icon: Settings, comp: markRaw(SettingsView), sub: '网络与程序设置' },
  { key: 'connections', label: '连接', icon: Activity, comp: markRaw(ConnectionsView), sub: '活动转发会话' },
  { key: 'logs', label: '日志', icon: ScrollText, comp: markRaw(LogsView), sub: '实时后端日志' },
  { key: 'about', label: '关于', icon: Info, comp: markRaw(AboutView), sub: '版本与更新' }
]
const active = ref('services')
const cur = computed(() => tabs.find((t) => t.key === active.value))
const body = shallowRef(ServicesView)

function onNav(t) {
  active.value = t.key
  body.value = t.comp
}

// 供各视图跳转到「设置」的指定子分区（复刻原版模式的「详细设置」入口）
function goSettings(subKey) {
  active.value = 'settings'
  const s = tabs.find((t) => t.key === 'settings')
  body.value = s.comp
  store.settingsSub = subKey || 'general'
}
provide('goSettings', goSettings)

const svcOn = () => {
  const s = store.status
  if (!s) return false
  const svcs = s.services || {}
  return svcs.fwd === 'active' || svcs.caddy === 'active'
}

const enabledCount = computed(() => store.rules.filter((r) => r.enabled).length)
const totalCount = computed(() => store.rules.length)
const versionText = () => store.version?.version || ''

const THEME_LABEL = { auto: '跟随系统', light: '浅色', dark: '深色' }
function nextTheme() {
  const order = ['auto', 'light', 'dark']
  const i = order.indexOf(store.theme)
  const next = order[(i + 1) % order.length]
  setTheme(next)
  const el = document.getElementById('toast')
  if (el) {
    el.textContent = '主题模式：' + THEME_LABEL[next]
    el.className = 'show ok'
    clearTimeout(el._t)
    el._t = setTimeout(() => (el.className = ''), 1800)
  }
}

// —— 界面缩放（复刻原版 uiScale；存 localStorage，浏览器侧生效）——
const SCALES = [90, 100, 110, 125]
const scale = ref(100)
function applyScale(v) {
  try {
    localStorage.setItem('s302-scale', String(v))
  } catch {}
  document.documentElement.style.zoom = String(v / 100)
}
function nextScale() {
  const i = SCALES.indexOf(scale.value)
  const v = SCALES[(i + 1) % SCALES.length]
  scale.value = v
  applyScale(v)
}

// —— 浏览器通知（可选，服务状态变化时提醒）——
const notifOn = ref(false)
function askNotif() {
  if (!('Notification' in window)) {
    const el = document.getElementById('toast')
    if (el) {
      el.textContent = '当前浏览器不支持通知'
      el.className = 'show warn'
      el._t = setTimeout(() => (el.className = ''), 2000)
    }
    return
  }
  if (Notification.permission === 'granted') {
    notifOn.value = !notifOn.value
  } else {
    Notification.requestPermission().then((p) => {
      if (p === 'granted') {
        notifOn.value = true
        new Notification('Steamcommunity 302', { body: '已开启服务状态通知' })
      }
    })
  }
  try {
    localStorage.setItem('s302-notif', notifOn.value ? '1' : '0')
  } catch {}
}

function quit() {
  window.close()
  setTimeout(() => {
    const el = document.getElementById('toast')
    if (el) {
      el.textContent = '浏览器已阻止自动关闭，请直接关闭本标签页'
      el.className = 'show warn'
      clearTimeout(el._t)
      el._t = setTimeout(() => (el.className = ''), 3200)
    }
  }, 150)
}

provide('reload', reloadAll)
const wrapStatus = async () => {
  await loadStatus()
  watchFailures(store.status)
}
provide('reloadStatus', wrapStatus)

// 服务状态通知：对比前后快照，检测服务转为失败/停止时提醒
let prevSvcs = null
function watchFailures(status) {
  if (!notifOn.value || !status || !status.services) return
  const svcs = status.services
  const names = { fwd: '端口转发', caddy: 'Caddy', dnsd: 'DNS 服务' }
  if (prevSvcs) {
    for (const k of Object.keys(names)) {
      const p = prevSvcs[k]
      const c = svcs[k]
      if (p === 'active' && c && c !== 'active') {
        new Notification(names[k] + ' 停止', { body: '检测到服务异常停止，请到 服务 页查看。' })
      }
    }
  }
  prevSvcs = { ...svcs }
}

onMounted(async () => {
  try {
    const saved = parseInt(localStorage.getItem('s302-scale') || '100', 10)
    if (SCALES.includes(saved)) {
      scale.value = saved
      applyScale(saved)
    }
  } catch {}
  try {
    notifOn.value = localStorage.getItem('s302-notif') === '1'
  } catch {}
  await reloadAll().catch(() => {})
  watchFailures(store.status)
})
</script>

<template>
  <div class="app">
    <header class="topbar">
      <div class="topbar-title">
        <img class="logo-img" :src="logoUrl" alt="" draggable="false" />
        <span class="title-main">Steamcommunity 302</span>
        <span class="title-ver">V{{ versionText() }}</span>
      </div>
      <div class="window-controls" role="group" aria-label="窗口控制">
        <button
          class="wc-btn"
          aria-label="通知"
          :title="notifOn ? '关闭服务状态通知' : '开启服务状态通知（需浏览器允许）'"
          @click="askNotif"
        >
          <span class="wc-dot" :class="{ on: notifOn }"></span>
        </button>
        <button class="wc-btn" aria-label="界面缩放比例" :title="'界面缩放：' + scale + '%'" @click="nextScale">
          <span class="t-label">{{ scale }}%</span>
        </button>
        <button
          class="wc-btn"
          :aria-label="'切换主题，当前' + THEME_LABEL[store.theme]"
          :title="'主题模式：' + THEME_LABEL[store.theme]"
          @click="nextTheme"
        >
          <span class="t-label">{{ THEME_LABEL[store.theme] }}</span>
        </button>
        <button class="wc-btn wc-quit" aria-label="退出 UI" title="退出 UI" @click="quit">
          <Power :size="15" />
        </button>
      </div>
    </header>

    <div class="frame">
      <aside class="sidebar">
        <nav class="primary-nav" aria-label="主导航">
          <button
            v-for="t in tabs"
            :key="t.key"
            class="nav-item"
            :class="{ active: active === t.key }"
            :aria-label="t.label"
            @click="onNav(t)"
          >
            <span class="primary-nav__indicator"></span>
            <component :is="t.icon" :size="16" />
            <span class="nav-text">{{ t.label }}</span>
            <span class="nav-state">{{ t.sub }}</span>
          </button>
        </nav>

        <div class="sidebar-footer" :class="{ stacked: true }">
          <div class="conn-status" aria-label="连接状态">
            <span class="status-dot" :class="{ on: svcOn() }"></span>
            <span class="conn">{{ svcOn() ? '已连接' : '未连接' }}</span>
            <span class="cnt">{{ enabledCount }}/{{ totalCount }} 规则启用</span>
          </div>
          <button class="quit" aria-label="退出 UI" @click="quit"><Power :size="14" /> 退出 UI</button>
        </div>
      </aside>

      <main class="main">
        <div class="content">
          <div class="wrap">
            <div class="hd">
              <div class="hd-txt">
                <h1 class="hd-title">{{ cur.label }}</h1>
                <span class="hd-sub">{{ cur.sub }}</span>
              </div>
            </div>
            <component :is="body" :key="active" />
          </div>
        </div>
        <footer class="statusbar">
          <div class="statusbar-group">
            <span class="statusbar-item" :class="{ 'statusbar-item--success': svcOn() }">
              <span class="status-dot" :class="{ on: svcOn() }"></span>
              连接状态：{{ svcOn() ? '已连接' : '未连接' }}
            </span>
          </div>
          <div class="statusbar-group">
            <span
              v-for="(s, k) in store.status?.services || {}"
              :key="k"
              class="statusbar-item"
              :class="s === 'active' ? 'statusbar-item--success' : s === 'failed' ? 'statusbar-item--warning' : ''"
            >
              {{ k }}：{{ s }}
            </span>
          </div>
          <div class="statusbar-group">
            <span class="statusbar-item">规则 {{ enabledCount }}/{{ totalCount }}</span>
            <span class="statusbar-item">版本 v{{ versionText() }}</span>
            <span class="statusbar-item">{{ scale }}% · {{ THEME_LABEL[store.theme] }}</span>
          </div>
        </footer>
      </main>
    </div>

    <div id="toast"></div>
  </div>
</template>

<style scoped>
.app {
  display: flex;
  flex-direction: column;
  height: 100vh;
  overflow: hidden;
  background: var(--color-bg);
}
/* —— 顶部标题栏（原版窗口区域） —— */
.topbar {
  flex: none;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 0 12px 0 14px;
  background: var(--color-bg-soft);
  border-bottom: 1px solid var(--color-border);
}
.topbar-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  -webkit-app-region: drag;
}
.logo-img {
  width: 26px;
  height: 26px;
  object-fit: contain;
  flex: none;
}
.title-main {
  color: var(--color-strong);
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
}
.title-ver {
  color: var(--color-faint);
  font-size: 11px;
  font-weight: 600;
  background: var(--color-hover);
  border: 1px solid var(--color-border);
  padding: 1px 7px;
  border-radius: 10px;
}
.window-controls {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
}
.wc-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 30px;
  height: 26px;
  border-radius: 6px;
  border: 1px solid transparent;
  background: transparent;
  color: var(--color-muted);
  font-size: 11.5px;
  cursor: pointer;
  transition: all 0.12s;
}
.wc-btn:hover {
  background: var(--color-hover);
  color: var(--color-strong);
}
.wc-btn.wc-quit:hover {
  background: var(--color-primary-quiet, var(--color-primary-dim));
  color: var(--color-off);
}
.wc-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--color-faint);
}
.wc-dot.on {
  background: var(--color-on);
  box-shadow: 0 0 6px rgba(101, 183, 122, 0.7);
}
.frame {
  flex: 1;
  min-height: 0;
  display: flex;
}
/* —— 主导航侧栏 —— */
.sidebar {
  width: 214px;
  flex: none;
  background: var(--color-bg-soft);
  border-right: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
}
.primary-nav {
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  overflow-y: auto;
}
.nav-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  border: 0;
  background: transparent;
  color: var(--color-muted);
  padding: 10px 12px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13.5px;
  font-weight: 500;
  transition: all 0.12s;
  text-align: left;
}
.nav-item:hover {
  background: var(--color-hover);
  color: var(--color-strong);
}
.nav-item svg {
  flex: none;
}
.nav-item.active {
  background: var(--color-primary-dim);
  color: var(--color-strong);
}
.nav-item.active svg {
  color: var(--color-primary-hi);
}
.primary-nav__indicator {
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%) scaleY(0);
  width: 3px;
  height: 18px;
  border-radius: 2px;
  background: var(--color-primary-hi);
  opacity: 0;
  transition: all 0.15s;
}
.nav-item.active .primary-nav__indicator {
  opacity: 1;
  transform: translateY(-50%) scaleY(1);
}
.nav-text {
  white-space: nowrap;
}
.nav-state {
  margin-left: auto;
  color: var(--color-faint);
  font-size: 10.5px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 74px;
}
.sidebar-footer {
  padding: 10px 10px 12px;
  border-top: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  gap: 9px;
}
.sidebar-footer--stacked {
  gap: 9px;
}
.conn-status {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
  color: var(--color-muted);
  padding: 0 4px;
  flex-wrap: wrap;
}
.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-faint);
  flex: none;
}
.status-dot.on {
  background: var(--color-on);
  box-shadow: 0 0 6px rgba(101, 183, 122, 0.6);
}
.conn {
  font-weight: 600;
}
.cnt {
  color: var(--color-faint);
  font-size: 11px;
}
.quit {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border: 1px solid var(--color-border);
  background: transparent;
  color: var(--color-muted);
  border-radius: 6px;
  padding: 8px;
  cursor: pointer;
  font-size: 12.5px;
  transition: all 0.12s;
}
.quit:hover {
  color: var(--color-strong);
  border-color: var(--color-primary);
}
.main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.hd {
  padding: 18px 26px 6px;
  flex: none;
}
.hd-title {
  margin: 0;
  color: var(--color-strong);
  font-size: 20px;
  font-weight: 700;
}
.hd-sub {
  color: var(--color-faint);
  font-size: 12px;
}
.content {
  flex: 1;
  overflow-y: auto;
  padding: 8px 26px 30px;
}
.wrap {
  max-width: 900px;
  margin: 0 auto;
}
/* —— 底部状态栏 —— */
.statusbar {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 5px 14px;
  border-top: 1px solid var(--color-border);
  background: var(--color-bg-soft);
  font-size: 11.5px;
  color: var(--color-faint);
}
.statusbar-group {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
}
.statusbar-item {
  display: flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.statusbar-item--success {
  color: var(--color-on);
}
.statusbar-item--warning {
  color: var(--color-warn);
}
#toast {
  position: fixed;
  left: 50%;
  bottom: 28px;
  transform: translateX(-50%) translateY(12px);
  background: var(--color-card);
  border: 1px solid var(--color-border);
  color: var(--color-fg);
  padding: 9px 18px;
  border-radius: 6px;
  font-size: 13px;
  opacity: 0;
  pointer-events: none;
  transition: all 0.25s;
  z-index: 99;
  box-shadow: 0 4px 12px var(--shadow-window);
}
#toast.show {
  opacity: 1;
  transform: translateX(-50%) translateY(0);
}
#toast.ok {
  border-color: var(--color-on);
}
#toast.err {
  border-color: var(--color-off);
}
@media (max-width: 760px) {
  .nav-state {
    display: none;
  }
}
</style>