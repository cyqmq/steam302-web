<script setup>
import { ref, onMounted, onBeforeUnmount, inject } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import CustomRadio from '../components/CustomRadio.vue'
import { get } from '../lib/api.js'
import { toast } from '../lib/state.js'

const reloadStatus = inject('reloadStatus', async () => {})

const mode = ref('tail') // tail | all
const lines = ref([])
const spinning = ref(false)
let timer = null

async function pull() {
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
  }
}

function stamp(line) {
  const m = String(line).match(/(\d{4}[\/-]\d{2}[\/-]\d{2}[ T]\d{2}:\d{2}:\d{2})/)
  return m ? m[1] : ''
}

function refresh() {
  spinning.value = true
  pull().finally(() => setTimeout(() => (spinning.value = false), 250))
}

function setMode(v) {
  mode.value = v
  lines.value = []
  pull()
}

onMounted(() => {
  pull()
  timer = setInterval(pull, 2000)
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
    <pre v-if="lines.length" class="term">
<code v-for="(l, i) in lines" :key="i"><span v-if="stamp(l)" class="ts">{{ stamp(l) }}</span><span>{{ ' ' + l }}</span>
</code></pre>
    <div v-else class="eof">暂无日志输出…</div>
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
  margin-bottom: 10px;
  flex-wrap: wrap;
  gap: 10px;
}
.term {
  background: #171516;
  border: 1px solid var(--color-border);
  border-radius: 8px;
  height: calc(100vh - 220px);
  overflow: auto;
  padding: 12px 14px;
  font: 12px/1.6 ui-monospace, SFMono-Regular, Consolas, monospace;
  color: #d6d6d2;
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
  border-radius: 8px;
}
.btn {
  border: 1px solid var(--color-border);
  background: transparent;
  color: var(--color-muted);
  border-radius: 8px;
  padding: 7px 13px;
  font-size: 12.5px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
.btn:hover {
  color: #fff;
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