<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Search, X, Power, Trash2, RefreshCw, Unplug } from 'lucide-vue-next'
import CustomRadio from '../components/CustomRadio.vue'

const tab = ref('active')
const q = ref('')
const data = ref({ active: [], recent: [], active_n: 0, total_up: 0, total_down: 0, running: true })
const state = ref('loading') // loading | ok | off | err
const errMsg = ref('')
let poll = null

const api = (p, opt) => fetch(p, { ...opt, headers: { 'Content-Type': 'application/json' } }).then((r) => r.json())

const load = async () => {
  try {
    const d = await api('/api/connections')
    if (!d || typeof d !== 'object') return
    if (d.ok === false) {
      state.value = 'off'
      errMsg.value = d.error || ''
      return
    }
    data.value = d
    state.value = 'ok'
  } catch (e) {
    state.value = 'err'
    errMsg.value = String(e)
  }
}

const refresh = () => load()

const rows = computed(() => {
  const src = tab.value === 'active' ? data.value.active : data.value.recent
  const kw = q.value.trim().toLowerCase()
  if (!kw) return src
  return (src || []).filter((c) => {
    const h = String(c.host || '')
    const r = String(c.rule || '')
    const s = String(c.remote || '')
    return h.toLowerCase().includes(kw) || r.toLowerCase().includes(kw) || s.toLowerCase().includes(kw)
  })
})

const fmtBytes = (n) => {
  n = Number(n) || 0
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(2) + ' MB'
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}

const dur = (start, end) => {
  const t = Date.parse(end) - Date.parse(start)
  if (!isFinite(t) || t < 0) return '–'
  const s = Math.floor(t / 1000)
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60
  if (h) return h + '时' + m + '分'
  return m ? m + '分' + sec + '秒' : sec + '秒'
}

const disconnect = async (id) => {
  await api('/api/connections/close', { method: 'POST', body: JSON.stringify({ id }) }).then(load)
}
const disconnectAll = async () => {
  await api('/api/connections/close-all', { method: 'POST', body: '{}' })
  load()
}
const clearRecent = async () => {
  await api('/api/connections/clear', { method: 'POST', body: '{}' })
  load()
}

onMounted(() => {
  load()
  poll = setInterval(load, 2000)
})
onBeforeUnmount(() => clearInterval(poll))
</script>

<template>
  <div class="conn-page">
    <div class="toolbar">
      <div class="radios">
        <CustomRadio v-model="tab" value="active" label="活动" @update:model-value="(v) => (tab = v)" />
        <CustomRadio v-model="tab" value="recent" label="最近" @update:model-value="(v) => (tab = v)" />
      </div>
      <div class="qbox">
        <Search :size="14" class="qi" />
        <input class="qinp" type="text" placeholder="搜索会话（主机名、规则、源地址）" v-model="q" />
        <button v-if="q" class="qclr" @click="q = ''"><X :size="13" /></button>
      </div>
      <div class="ops">
        <button class="btn" :disabled="tab !== 'active'" @click="disconnectAll"><Unplug :size="13" /> 断开所有活动会话</button>
        <button class="btn" @click="refresh"><RefreshCw :size="13" /> 刷新</button>
        <button class="btn" :disabled="!(data.recent || []).length" @click="clearRecent"><Trash2 :size="13" /> 清空历史</button>
      </div>
    </div>

    <div v-if="state === 'ok'" class="table">
      <div class="thead">
        <span>主机名 / 规则</span><span>源地址</span><span class="num">客户端→上游</span>
        <span class="num">上游→客户端</span><span class="num">持续</span><span class="op">操作</span>
      </div>
      <div v-if="!rows.length" class="empty">
        <div class="us-ico"><Power :size="20" /></div>
        <p>{{ tab === 'active' ? '当前没有活动转发会话' : '暂无最近的转发会话' }}</p>
        <p class="us-sub">访问被 S302 劫持的站点后会自动出现在这里（识别 TLS SNI / Host 头）。</p>
      </div>
      <div v-for="c in rows" :key="c.id" class="crow">
        <span class="host">
          <span class="hn">{{ c.host || '（未识别）' }}</span>
          <span v-if="c.rule" class="rule-tag">{{ c.rule }}</span>
          <span v-if="c.closed" class="closed-tag">已结束</span>
        </span>
        <span class="lbl">{{ c.remote }}</span>
        <span class="num">{{ fmtBytes(c.up) }}</span>
        <span class="num">{{ fmtBytes(c.down) }}</span>
        <span class="num">{{ dur(c.start, c.closed ? c.end : new Date().toISOString()) }}</span>
        <span class="op">
          <button v-if="!c.closed && tab === 'active'" class="mini" title="强制断开" @click="disconnect(c.id)">
            <Unplug :size="13" />
          </button>
          <span v-else class="fh">–</span>
        </span>
      </div>
    </div>

    <div v-else-if="state === 'off'" class="table">
      <div class="unsupported">
        <div class="us-ico warn"><Power :size="22" /></div>
        <p>转发服务未运行，无法读取连接监控</p>
        <p class="us-sub">{{ errMsg }}。请先启动服务（服务页 → 启动服务）。</p>
      </div>
    </div>

    <div v-else class="table">
      <div class="unsupported">
        <div class="us-ico warn"><RefreshCw :size="22" /></div>
        <p>{{ state === 'loading' ? '正在读取转发会话…' : '连接监控读取失败' }}</p>
        <p class="us-sub">{{ errMsg }}</p>
        <button class="btn" style="margin-top: 14px" @click="refresh">重试</button>
      </div>
    </div>

    <p class="foot">当前活动 {{ data.active_n }} · 累计 客户端→上游 {{ fmtBytes(data.total_up) }} / 上游→客户端 {{ fmtBytes(data.total_down) }} · 每 2 秒自动刷新</p>
  </div>
</template>

<style scoped>
.conn-page {
  max-width: 900px;
  margin: 0 auto;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}
.radios {
  display: flex;
  gap: 6px;
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
  min-width: 220px;
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
.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.table {
  border: 1px solid var(--color-border);
  border-radius: 8px;
  overflow: hidden;
  background: var(--color-card);
}
.thead {
  display: grid;
  grid-template-columns: 2.4fr 1.6fr 1.1fr 1.1fr 0.9fr 0.7fr;
  gap: 8px;
  padding: 10px 14px;
  background: var(--color-card-head);
  border-bottom: 1px solid var(--color-border);
  color: var(--color-muted);
  font-size: 11.5px;
  font-weight: 600;
}
.crow {
  display: grid;
  grid-template-columns: 2.4fr 1.6fr 1.1fr 1.1fr 0.9fr 0.7fr;
  gap: 8px;
  align-items: center;
  padding: 9px 14px;
  border-bottom: 1px solid var(--color-border);
  font-size: 12.5px;
  color: var(--color-fg);
}
.crow:last-child {
  border-bottom: 0;
}
.num {
  text-align: right;
  font-variant-numeric: tabular-nums;
  color: var(--color-muted);
}
.op {
  text-align: right;
}
.host {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
}
.hn {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.rule-tag {
  flex: none;
  border: 1px solid var(--color-border);
  border-radius: 4px;
  padding: 1px 6px;
  font-size: 10.5px;
  color: var(--color-primary);
  background: var(--color-primary-dim);
  white-space: nowrap;
}
.closed-tag {
  flex: none;
  border-radius: 4px;
  padding: 1px 6px;
  font-size: 10.5px;
  color: var(--color-faint);
  border: 1px solid var(--color-border);
}
.mini {
  border: 1px solid var(--color-border);
  background: transparent;
  color: var(--color-muted);
  border-radius: 5px;
  padding: 4px 7px;
  cursor: pointer;
  display: inline-flex;
}
.mini:hover {
  color: var(--color-primary);
  border-color: var(--color-primary);
}
.fh {
  color: var(--color-faint);
}
.empty {
  padding: 40px 18px;
  text-align: center;
  color: var(--color-faint);
}
.unsupported {
  padding: 46px 18px;
  text-align: center;
  color: var(--color-faint);
}
.us-ico {
  width: 46px;
  height: 46px;
  margin: 0 auto 12px;
  border-radius: 50%;
  background: var(--color-primary-dim);
  color: var(--color-primary);
  display: flex;
  align-items: center;
  justify-content: center;
}
.us-ico.warn {
  background: var(--color-warn-dim, var(--color-primary-dim));
  color: var(--color-warn, var(--color-primary));
}
.unsupported p,
.empty p {
  margin: 4px 0;
  font-size: 13px;
  color: var(--color-fg);
}
.unsupported .us-sub,
.empty .us-sub {
  color: var(--color-faint);
  font-size: 11.5px;
}
.foot {
  margin: 10px 2px 0;
  font-size: 11.5px;
  color: var(--color-faint);
}
</style>