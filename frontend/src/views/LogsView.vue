<script setup>
import { ref, computed, onMounted, onBeforeUnmount, inject } from 'vue'
import { RefreshCw, Pause, Play, Trash2, Download, Copy, Search, X } from 'lucide-vue-next'
import CustomRadio from '../components/CustomRadio.vue'
import CustomSelect from '../components/CustomSelect.vue'
import CustomCheckbox from '../components/CustomCheckbox.vue'
import { get } from '../lib/api.js'
import { toast } from '../lib/state.js'

const reloadStatus = inject('reloadStatus', async () => {})

const mode = ref('tail') // tail | all
const lines = ref([])
const spinning = ref(false)
const paused = ref(false)
const lv = ref('all')
const kw = ref('')
const wildcard = ref(false)
const filtDns = ref(false)
let timer = null

const LEVEL_ORDER = ['debug', 'info', 'warn', 'error']

const LEVEL_OPTIONS = [
  { value: 'all', label: '全部等级' },
  { value: 'debug', label: '调试' },
  { value: 'info', label: '信息' },
  { value: 'warn', label: '警告' },
  { value: 'error', label: '错误' }
]

function matchLevel(line) {
  if (lv.value === 'all') return true
  const low = line.toLowerCase()
  const hasTag = LEVEL_ORDER.some((k) => low.includes(k))
  if (hasTag) return low.includes(lv.value)
  return lv.value === 'info'
}

function matchDns(line) {
  return !filtDns.value || !/dns|query|resolver/i.test(line)
}

function globToRegExp(s) {
  return new RegExp(s.split('*').map((p) => p.replace(/[.+?^${}()|[\]\\]/g, '\\$&')).join('.*'), 'i')
}
function matchKw(line) {
  if (!kw.value.trim()) return true
  return wildcard.value
    ? globToRegExp(kw.value).test(line)
    : line.toLowerCase().includes(kw.value.toLowerCase())
}

const shown = computed(() =>
  lines.value.filter((l) => matchLevel(l) && matchDns(l) && matchKw(l))
)

async function pull(raw) {
  if (raw) spinning.value = true
  try {
    const d = await get('/api/logs?all=' + (mode.value === 'all' ? 1 : 0))
    if (d && d.lines && d.lines.length) {
      const merged =
        JSON.stringify(lines.value.slice(-20)) === JSON.stringify(d.lines.slice(-20))
          ? lines.value
          : d.lines
      lines.value = merged
    } else {
      lines.value = []
    }
  } catch {
    lines.value.unshift('// 读入日志失败：请确认后端已运行')
    if (lines.value.length > 600) lines.value.pop()
  } finally {
    if (raw) setTimeout(() => (spinning.value = false), 250)
  }
}

function stamp(line) {
  const m = String(line).match(/(\d{4}[\/-]\d{2}[\/-]\d{2}[ T]\d{2}:\d{2}:\d{2})/)
  return m ? m[1] : ''
}

function refresh() {
  if (paused.value) paused.value = false
  pull(true)
}

function setMode(v) {
  mode.value = v
  lines.value = []
  pull()
}

function togglePause() {
  paused.value = !paused.value
}

function clearLogs() {
  lines.value = []
}

function exportLogs() {
  const blob = new Blob([shown.value.join('\n')], { type: 'text/plain;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = 'steam302-webui.log'
  a.click()
  URL.revokeObjectURL(a.href)
}

async function copyFull() {
  try {
    const d = await get('/api/logs?all=1')
    const text = (d && d.lines ? d.lines : []).join('\n')
    await navigator.clipboard.writeText(text || '(空)')
    toast('完整日志已复制到剪贴板')
  } catch (e) {
    toast('复制失败: ' + e.message, 'err')
  }
}

onMounted(() => {
  pull()
  timer = setInterval(() => {
    if (!paused.value && !document.hidden) pull()
  }, 2000)
  reloadStatus()
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="log-page">
    <div class="lhead">
      <div class="radios">
        <CustomRadio v-model="mode" value="tail" label="实时 1000 行" @update:model-value="setMode" />
        <CustomRadio v-model="mode" value="all" label="显示全部" @update:model-value="setMode" />
      </div>
      <button class="btn ghostb" :disabled="spinning" @click="refresh">
        <RefreshCw :size="13" :class="{ spin: spinning }" /> 刷新
      </button>
    </div>

    <div class="ftool">
      <div class="qbox">
        <Search :size="14" class="qi" />
        <input class="qinp" type="text" placeholder="搜索日志" v-model="kw" />
        <button v-if="kw" class="qclr" @click="kw = ''"><X :size="13" /></button>
      </div>
      <CustomSelect :model-value="lv" :options="LEVEL_OPTIONS" @change="(e) => (lv = e.value)" />
      <CustomCheckbox label="通配符" :model-value="wildcard" @change="(v) => (wildcard = v)" />
      <CustomCheckbox label="过滤DNS查询" :model-value="filtDns" @change="(v) => (filtDns = v)" />
      <button class="btn ghostb" @click="togglePause">
        <Pause v-if="!paused" :size="13" /><Play v-else :size="13" /> {{ paused ? '继续' : '暂停' }}
      </button>
      <button class="btn ghostb" @click="clearLogs"><Trash2 :size="13" /> 清空</button>
      <button class="btn ghostb" @click="exportLogs"><Download :size="13" /> 导出</button>
      <button class="btn ghostb" @click="copyFull"><Copy :size="13" /> 复制完整日志</button>
    </div>

    <pre v-if="shown.length" class="term">
<code v-for="(l, i) in shown" :key="i"><span v-if="stamp(l)" class="ts">{{ stamp(l) }}</span><span>{{ ' ' + l }}</span>
</code></pre>
    <div v-else class="eof">暂无匹配的日志输出…</div>
  </div>
</template>

<style scoped>
.log-page {
  max-width: 900px;
  margin: 0 auto;
}
.lhead {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
  flex-wrap: wrap;
  gap: 10px;
}
.ftool {
  display: flex;
  align-items: center;
  gap: 8px;
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
  min-width: 180px;
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
.term {
  background: var(--color-term-bg);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  height: calc(100vh - 270px);
  overflow: auto;
  padding: 12px 14px;
  font: 12px/1.6 ui-monospace, SFMono-Regular, Consolas, monospace;
  color: var(--color-term-fg);
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}
.ts {
  color: var(--color-faint);
}
.eof {
  text-align: center;
  color: var(--color-faint);
  font-size: 13px;
  padding: 40px;
  border: 1px dashed var(--color-border);
  border-radius: 6px;
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
.btn:disabled {
  opacity: 0.5;
}
.spin {
  animation: rot 0.8s linear infinite;
}
@keyframes rot {
  to {
    transform: rotate(360deg);
  }
}
</style>