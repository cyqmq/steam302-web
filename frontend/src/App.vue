<script setup>
import { ref, shallowRef, provide, markRaw, onMounted, computed } from 'vue'
import { Server, Settings, ScrollText, Info, Power, Repeat, Circle } from 'lucide-vue-next'
import ServicesView from './views/ServicesView.vue'
import SettingsView from './views/SettingsView.vue'
import LogsView from './views/LogsView.vue'
import AboutView from './views/AboutView.vue'
import { reloadAll, loadStatus } from './lib/data.js'
import { store } from './lib/state.js'

const tabs = [
  { key: 'services', label: '服务', icon: Server, comp: markRaw(ServicesView), sub: '开关代理规则 · 服务控制' },
  { key: 'settings', label: '设置', icon: Settings, comp: markRaw(SettingsView), sub: '程序行为 · 网络 · 证书' },
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

const versionText = () => store.version?.version || ''

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
provide('reloadStatus', loadStatus)

onMounted(() => {
  reloadAll().catch(() => {})
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
          {{ svcOn() ? '服务运行中' : '服务未运行' }}
        </div>
        <button class="quit" @click="quit"><Power :size="14" /> 退出UI</button>
      </div>
    </aside>

    <main class="main">
      <header class="hd">
        <div>
          <h1 class="hd-title">{{ cur.label }}</h1>
          <span class="hd-sub">{{ cur.sub }}</span>
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
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}
.bt {
  color: #fff;
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
  background: rgba(255, 255, 255, 0.05);
  color: #fff;
}
.navit.active {
  background: var(--color-primary-dim);
  color: #fff;
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
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-faint);
}
.dot.on {
  background: var(--color-on);
  box-shadow: 0 0 6px rgba(102, 187, 106, 0.6);
}
.quit {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border: 1px solid var(--color-border);
  background: transparent;
  color: var(--color-muted);
  border-radius: 8px;
  padding: 8px;
  cursor: pointer;
  font-size: 12.5px;
  transition: all 0.12s;
}
.quit:hover {
  color: #fff;
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
}
.hd-title {
  margin: 0;
  color: #fff;
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