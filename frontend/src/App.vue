<script setup>
import { ref, shallowRef, provide, markRaw, onMounted, computed } from 'vue'
import { Server, Settings, Activity, ScrollText, Info, Power, Repeat } from 'lucide-vue-next'
import ServicesView from './views/ServicesView.vue'
import SettingsView from './views/SettingsView.vue'
import ConnectionsView from './views/ConnectionsView.vue'
import LogsView from './views/LogsView.vue'
import AboutView from './views/AboutView.vue'
import { reloadAll, loadStatus } from './lib/data.js'
import { store, setTheme } from './lib/state.js'

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
    <aside class="side">
      <div class="brand">
        <span class="logo"><Repeat :size="17" /></span>
        <div class="bn">
          <div class="bt">Steamcommunity 302</div>
          <div class="bs">Web 管理端 · v{{ versionText() }}</div>
        </div>
      </div>

      <nav class="nav">
        <button
          v-for="t in tabs"
          :key="t.key"
          class="navit"
          :class="{ active: active === t.key }"
          @click="onNav(t)"
        >
          <component :is="t.icon" :size="17" />
          <span>{{ t.label }}</span>
        </button>
      </nav>

      <div class="side-foot">
        <div class="svc">
          <span class="dot" :class="{ on: svcOn() }"></span>
          <span class="conn">{{ svcOn() ? '已连接' : '未连接' }}</span>
          <span class="cnt">{{ enabledCount }}/{{ totalCount }} 规则启用</span>
        </div>
        <button class="quit" @click="quit"><Power :size="14" /> 退出UI</button>
      </div>
    </aside>

    <main class="main">
      <header class="hd">
        <div class="hd-txt">
          <h1 class="hd-title">{{ cur.label }}</h1>
          <span class="hd-sub">{{ cur.sub }}</span>
        </div>
        <div class="hd-ops">
          <button class="theme" :title="'界面缩放：' + scale + '%'" @click="nextScale">
            <span class="t-label">{{ scale }}%</span>
          </button>
          <button class="theme" :title="notifOn ? '关闭服务状态通知' : '开启服务状态通知（需浏览器允许）'" @click="askNotif">
            <span class="t-label">{{ notifOn ? '通知:开' : '通知' }}</span>
          </button>
          <button class="theme" :title="'主题模式：' + THEME_LABEL[store.theme]" @click="nextTheme">
            <span class="t-label">{{ THEME_LABEL[store.theme] }}</span>
          </button>
        </div>
      </header>
      <div class="content">
        <div class="wrap">
          <component :is="body" :key="active" />
        </div>
      </div>
    </main>

    <div id="toast"></div>
  </div>
</template>

<style scoped>
.app {
  display: flex;
  height: 100vh;
  overflow: hidden;
}
.side {
  width: 216px;
  flex: none;
  background: var(--color-bg-soft);
  border-right: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 16px 15px;
}
.logo {
  width: 34px;
  height: 34px;
  border-radius: 6px;
  background: linear-gradient(135deg, var(--color-primary-hi), var(--color-primary));
  color: var(--on-accent);
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}
.bt {
  color: var(--color-strong);
  font-weight: 700;
  font-size: 13.5px;
  line-height: 1.25;
}
.bs {
  color: var(--color-faint);
  font-size: 11px;
  margin-top: 2px;
}
.nav {
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 3px;
  flex: 1;
}
.navit {
  display: flex;
  align-items: center;
  gap: 11px;
  border: 0;
  background: transparent;
  color: var(--color-muted);
  padding: 10px 13px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13.5px;
  font-weight: 500;
  transition: all 0.12s;
  text-align: left;
}
.navit:hover {
  background: var(--color-hover);
  color: var(--color-strong);
}
.navit.active {
  background: var(--color-primary-dim);
  color: var(--color-strong);
}
.navit.active svg {
  color: var(--color-primary-hi);
}
.side-foot {
  padding: 12px 10px;
  border-top: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  gap: 9px;
}
.svc {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
  color: var(--color-muted);
  padding: 0 4px;
  flex-wrap: wrap;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-faint);
}
.dot.on {
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
  padding: 18px 26px 4px;
  flex: none;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
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
.hd-ops {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
}
.theme {
  border: 1px solid var(--color-border);
  background: var(--color-card);
  border-radius: 6px;
  color: var(--color-muted);
  font-size: 12px;
  padding: 6px 12px;
  cursor: pointer;
  flex: none;
}
.theme:hover {
  color: var(--color-primary-hi);
  border-color: var(--color-primary);
}
.content {
  flex: 1;
  overflow-y: auto;
  padding: 14px 26px 30px;
}
.wrap {
  max-width: 900px;
  margin: 0 auto;
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
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
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
</style>