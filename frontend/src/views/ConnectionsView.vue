<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { Search, X, Pause, Play, Trash2, Wifi, WifiOff, Loader2, Unplug } from 'lucide-vue-next'
import CustomSelect from '../components/CustomSelect.vue'
import { store, toast } from '../lib/state.js'

const levelOpts = [
  { value: 'all', label: '全部等级' },
  { value: 'DEBUG', label: '调试' },
  { value: 'INFO', label: '信息' },
  { value: 'WARN', label: '警告' },
  { value: 'ERROR', label: '错误' }
]

const level = ref('all')
const q = ref('')
const paused = ref(false)
const events = ref([])
const status = ref('connecting') // connecting | live | offline
const statusMsg = ref('')
const viewport = ref(null)
const stickBottom = ref(true)
let poll = null
let maxLines = 2000

const api = (p, opt) => fetch(p, { ...opt, headers: { 'Content-Type': 'application/json' } }).then((r) => r.json())

const load = async (initial) => {
  try {
    const d = await api('/api/connections/events')
    const live = !!(d && d.running)
    if (!live) {
      status.value = 'offline'
      statusMsg.value = (d && d.error) || ''
      return
    }
    if (d.ok === false) {
      status.value = 'offline'
      statusMsg.value = d.error || ''
      return
    }
    status.value = 'live'
    const seen = new Set()
    for (const ev of (events.value || [])) seen.add(ev.id + '@' + ev.at)
    const fresh = []
    for (const ev of (d.events || [])) {
      const k = ev.id + '@' + ev.at
      if (!seen.has(k)) {
        seen.add(k)
        fresh.push(ev)
      }
    }
    if (!paused.value && fresh.length) {
      events.value = events.value.concat(fresh)
      if (events.value.length > maxLines) events.value = events.value.slice(-maxLines)
      if (stickBottom.value) scrollBottom()
    }
  } catch (e) {
    if (initial) {
      status.value = 'offline'
      statusMsg.value = String(e)
    }
  }
}

async function scrollBottom() {
  await nextTick()
  if (viewport.value) viewport.value.scrollTop = viewport.value.scrollHeight
}

function onScroll() {
  const v = viewport.value
  if (!v) return
  stickBottom.value = v.scrollHeight - v.scrollTop - v.clientHeight < 24
}

function togglePause() {
  paused.value = !paused.value
  if (!paused.value) scrollBottom()
}

async function clearAll() {
  events.value = []
  toast('本地日志视图已清空；后续事件会继续显示。')
  await api('/api/connections/clear', { method: 'POST', body: '{}' }).catch(() => {})
}

async function disconnectAll() {
  try {
    await api('/api/connections/close-all', { method: 'POST', body: '{}' })
    toast('已请求断开所有活动会话')
  } catch (e) {
    toast('操作失败: ' + e.message, 'err')
  }
}

const filtered = computed(() => {
  const kw = q.value.trim().toLowerCase()
  return events.value.filter((ev) => {
    if (level.value !== 'all' && ev.level !== level.value) return false
    if (!kw) return true
    return [ev.host, ev.remote, ev.kind, ev.rule, ev.level].some((f) => String(f || '').toLowerCase().includes(kw))
  })
})

const fmtBytes = (n) => {
  n = Number(n) || 0
  if (n < 1024) return n + ' B'
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1073741824) return (n / 1048576).toFixed(2) + ' MB'
  return (n / 1073741824).toFixed(2) + ' GB'
}

const fmtTime = (iso) => {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  const ms = String(d.getMilliseconds()).padStart(3, '0')
  return `${hh}:${mm}:${ss}.${ms}`
}

const fmtDur = (ms) => {
  ms = Number(ms) || 0
  const s = Math.floor(ms / 1000)
  if (s < 1) return ms + ' ms'
  if (s < 60) return s + ' 秒'
  const m = Math.floor(s / 60)
  if (m < 60) return m + ' 分 ' + (s % 60) + ' 秒'
  return Math.floor(m / 60) + ' 时 ' + (m % 60) + ' 分'
}

const msgOf = (ev) => {
  if (ev.kind === 'close') {
    const host = ev.host ? `断开 ${ev.host}` : '连接已断开'
    const rule = ev.rule ? ` · 规则: ${ev.rule}` : ''
    const dur = ev.dur_ms >= 0 ? ` · 持续 ${fmtDur(ev.dur_ms)}` : ''
    const bytes = ` · 上行 ${fmtBytes(ev.up)} · 下行 ${fmtBytes(ev.down)}`
    return host + rule + dur + bytes
  }
  if (ev.kind === 'denied') return `连接失败：无法连接上游（${ev.host || ev.remote}）`
  const rule = ev.rule ? ` · 规则: ${ev.rule}` : ''
  return (ev.host ? `建立连接 ${ev.host}` : '建立连接（目标未识别）') + rule
}

const levelCN = (lv) => String(lv || 'INFO').toLowerCase()

const statusLabel = computed(() =>
  ({ connecting: '连接中', live: '实时', offline: '离线' }[status.value] || '未知')
)

onMounted(() => {
  load(true)
  poll = setInterval(() => load(false), 1500)
})
onBeforeUnmount(() => clearInterval(poll))
</script>

<template>
  <div class="conn-page">
    <div class="toolbar">
      <CustomSelect :model-value="level" :options="levelOpts" @change="(e) => (level = e.value)" />
      <div class="qbox">
        <Search :size="14" class="qi" />
        <input class="qinp" type="text" placeholder="查找（主机、来源、规则、等级）" v-model="q" />
        <button v-if="q" class="qclr" @click="q = ''"><X :size="13" /></button>
      </div>
      <div class="ops">
        <button class="btn ghostb" @click="togglePause">
          <Pause v-if="!paused" :size="13" /><Play v-else :size="13" /> {{ paused ? '继续' : '暂停' }}
        </button>
        <button class="btn ghostb" @click="clearAll"><Trash2 :size="13" /> 清空</button>
        <button class="btn ghostb" title="断开所有活动转发会话" @click="disconnectAll"><Unplug :size="13" /> 断开全部活动</button>
      </div>
      <span class="pill" :class="status">
        <Wifi v-if="status === 'live'" :size="13" />
        <Loader2 v-else-if="status === 'connecting'" :size="13" class="spin" />
        <WifiOff v-else :size="13" />
        {{ statusLabel }}
      </span>
    </div>

    <div class="table">
      <div class="thead" aria-hidden="true">
        <span>时间</span><span>等级</span><span>事件 / 消息</span>
      </div>

      <div v-if="!filtered.length" class="log-empty">
        <span class="le-txt">
          <strong>{{ events.length ? '没有符合当前筛选条件的日志。' : status === 'live' ? '暂无日志。' : '尚未连接日志流。' }}</strong>
        </span>
        <span class="le-sub">调整筛选条件或等待新事件。{{ status === 'offline' && statusMsg ? '（' + statusMsg + '）' : '' }}</span>
        <span v-if="status === 'offline'" class="le-sub">请先启动服务；连接监控依赖 s302fwd 回环管理接口。</span>
      </div>

      <div
        v-else
        ref="viewport"
        class="log-viewport"
        role="log"
        aria-label="连接日志列表"
        @scroll="onScroll"
      >
        <div v-for="(ev, i) in filtered" :key="ev.id + '@' + ev.at + '-' + i" class="line" :class="levelCN(ev.level)">
          <span class="lt">{{ fmtTime(ev.at) }}</span>
          <span class="lv"><i :class="levelCN(ev.level)">{{ ev.level || 'INFO' }}</i></span>
          <span class="lm">{{ msgOf(ev) }}</span>
          <span class="lr">{{ ev.remote }}</span>
        </div>
      </div>
    </div>

    <p class="foot">
      {{ paused ? '视图已暂停；后台仍继续接收有界日志流。' : status === 'offline' ? '后端未连接，正在显示最近日志。' : (store.status && store.status.fwd_active ? '实时连接日志流' : '') }}
      · 共 {{ filtered.length }} 条{{ q || level !== 'all' ? '（筛选后）' : '' }}
    </p>
  </div>
</template>

<style scoped>
.conn-page {
  max-width: 960px;
  margin: 0 auto;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
  flex-wrap: wrap;
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
.btn {
  border: 1px solid var(--color-border);
  background: var(--color-card);
  color: var(--color-muted);
  border-radius: 6px;
  padding: 7px 13px;
  font-size: 12.5px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
.btn:hover {
  color: var(--color-strong);
  border-color: var(--color-primary);
}
.pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--color-muted);
  border: 1px solid var(--color-border);
  border-radius: 20px;
  padding: 5px 11px;
  background: var(--color-card);
}
.pill.live {
  color: var(--color-on);
  border-color: var(--color-on);
}
.pill.connecting {
  color: var(--color-warn, var(--color-muted));
}
.spin {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.table {
  border: 1px solid var(--color-border);
  border-radius: 8px;
  overflow: hidden;
  background: var(--color-card);
  display: flex;
  flex-direction: column;
  height: calc(100vh - 210px);
  min-height: 280px;
}
.thead {
  display: grid;
  grid-template-columns: 130px 64px 1fr 200px;
  gap: 10px;
  padding: 9px 14px;
  background: var(--color-card-head);
  border-bottom: 1px solid var(--color-border);
  color: var(--color-muted);
  font-size: 11.5px;
  font-weight: 600;
  flex: none;
}
.log-viewport {
  flex: 1;
  overflow: auto;
  padding: 6px 0;
  font-family: ui-monospace, Consolas, Menlo, monospace;
  font-size: 12.5px;
}
.line {
  display: grid;
  grid-template-columns: 130px 64px 1fr 200px;
  gap: 10px;
  padding: 4px 14px;
  line-height: 1.6;
  border-bottom: 1px solid var(--color-border-soft);
  color: var(--color-fg);
}
.lt {
  color: var(--color-faint);
  font-variant-numeric: tabular-nums;
}
.lt::before {
  content: '';
}
.lr {
  color: var(--color-faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: right;
}
.lv i {
  font-style: normal;
  padding: 1px 7px;
  border-radius: 4px;
  font-size: 11px;
  border: 1px solid var(--color-border);
  color: var(--color-muted);
}
.lv i.info {
  color: var(--color-on);
  border-color: var(--color-on);
}
.lv i.warn {
  color: var(--color-warn);
  border-color: var(--color-warn);
}
.lv i.error {
  color: var(--color-error);
  border-color: var(--color-error);
}
.lm {
  min-width: 0;
  word-break: break-all;
}
.log-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--color-faint);
  text-align: center;
  padding: 24px;
}
.log-empty .le-txt {
  font-size: 13.5px;
  color: var(--color-fg);
}
.log-empty .le-sub {
  font-size: 12px;
  color: var(--color-faint);
  max-width: 520px;
}
.foot {
  margin: 10px 2px 0;
  font-size: 11.5px;
  color: var(--color-faint);
}
</style>