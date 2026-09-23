<script setup>
import { ref } from 'vue'
import { Repeat, RefreshCw, Download, ExternalLink, Loader2 } from 'lucide-vue-next'
import { store, toast } from '../lib/state.js'
import { get, post } from '../lib/api.js'
import { loadVersion } from '../lib/data.js'

const busy = ref(false)
const updating = ref(false)
const updMsg = ref('')

const v = () => store.version || {}

async function check() {
  busy.value = true
  try {
    await loadVersion()
    if (v().has_update && v().latest) {
      toast('发现新版本 v' + v().latest)
    } else {
      toast('已是最新版本')
    }
  } catch {
    toast('检查更新失败', 'err')
  } finally {
    busy.value = false
  }
}

// —— 自动更新（POST /api/update/start → 轮询状态）——
const PHASE_LABEL = {
  idle: '',
  checking: '正在检查版本…',
  downloading: '正在下载发布包…',
  verifying: '正在校验 sha256…',
  swapping: '正在备份并替换二进制…',
  restarting: '正在重启服务…',
  done: '更新完成',
  error: '更新失败'
}
async function doUpdate() {
  updating.value = true
  updMsg.value = '正在启动后台更新进程…'
  try {
    const d = await post('/api/update/start', {})
    if (!d || !d.started) {
      updating.value = false
      toast((d && (d.error || d.hint)) || '启动更新失败', 'err')
      updMsg.value = ''
      return
    }
    pollUpdate()
  } catch (e) {
    updating.value = false
    toast('启动更新失败: ' + e.message, 'err')
    updMsg.value = ''
  }
}
function pollUpdate() {
  const t = setTimeout(async () => {
    try {
      const st = await get('/api/update/status')
      updMsg.value = (st && PHASE_LABEL[st.phase]) || st?.message || ''
      if (st?.done) {
        updating.value = false
        if (st.ok) {
          toast('已升级到 v' + (st.latest || '') + '，请刷新页面')
          setTimeout(() => location.reload(), 1200)
        } else {
          toast(st.error || '更新失败', 'err')
        }
        return
      }
      pollUpdate()
    } catch {
      // webui 重启中：短暂后重试
      setTimeout(pollUpdate, 2000)
    }
  }, 1200)
}

function home() {
  const url = v().homepage || 'https://github.com/cyqmq/steam302-web'
  window.open(url, '_blank')
}
</script>

<template>
  <div class="ab">
    <div class="crd">
      <div class="mark"><Repeat :size="26" /></div>
      <h1 class="nm">Steamcommunity 302 Web</h1>
      <p class="ds">steamcommunity 302 网页管理端 — 前端 Vue3 复刻原版 Steamcommunity 302 桌面版</p>

      <div class="kv">
        <div class="li"><span>版本</span><b>v{{ v().version || '2.0.0' }}</b></div>
        <div class="li"><span>作者</span><b>{{ v().author || 'cyqmq' }}</b></div>
        <div class="li"><span>开源许可</span><b>MIT</b></div>
        <div class="li"><span>运行时</span><b>Go / caddy / systemd</b></div>
        <div class="li">
          <span>更新状态</span>
          <b v-if="v().has_update" class="up">发现新版本 v{{ v().latest }}</b>
          <b v-else>已是最新</b>
        </div>
      </div>

      <div class="ops">
        <button class="btn" :disabled="busy || updating" @click="check">
          <RefreshCw :size="14" :class="{ spin: busy }" /> 检查更新
        </button>
        <button
          v-if="v().has_update && v().latest && !updating"
          class="btn upbtn"
          @click="doUpdate"
        >
          <Download :size="14" /> 自动更新到 v{{ v().latest }}
        </button>
        <button class="btn sec" @click="home">
          <ExternalLink :size="14" /> 项目主页
        </button>
      </div>
      <p v-if="updating" class="upd"><Loader2 :size="13" class="spin" /> {{ updMsg }}</p>

      <p class="lic">
        By.羽翼城|Dogfight360 · Donate / 打赏开发者 — 本 Web 管理端为兼容复刻，功能以 Web 后端支持范围为准。
      </p>
    </div>
  </div>
</template>

<style scoped>
.ab {
  display: flex;
  justify-content: center;
  padding-top: 26px;
}
.crd {
  width: min(520px, 100%);
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  padding: 30px 32px;
  text-align: center;
}
.mark {
  width: 60px;
  height: 60px;
  margin: 0 auto 16px;
  border-radius: 14px;
  background: linear-gradient(135deg, var(--color-primary-hi), var(--color-primary));
  color: var(--on-accent);
  display: flex;
  align-items: center;
  justify-content: center;
}
.nm {
  margin: 0;
  color: var(--color-strong);
  font-size: 20px;
}
.ds {
  margin: 8px 0 0;
  color: var(--color-muted);
  font-size: 12.5px;
}
.kv {
  margin: 26px 0 20px;
  border-top: 1px solid var(--color-border-soft);
}
.li {
  display: flex;
  justify-content: space-between;
  padding: 9px 2px;
  border-bottom: 1px solid var(--color-border-soft);
  font-size: 13px;
}
.li span {
  color: var(--color-muted);
}
.li b {
  color: var(--color-fg);
  font-weight: 600;
}
.li b.up {
  color: var(--color-primary-hi);
}
.ops {
  display: flex;
  justify-content: center;
  gap: 10px;
  flex-wrap: wrap;
}
.btn {
  border: 0;
  border-radius: 6px;
  background: linear-gradient(135deg, var(--color-primary-hi), var(--color-primary));
  color: var(--on-accent);
  font-weight: 600;
  font-size: 13px;
  padding: 9px 18px;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  cursor: pointer;
}
.btn:disabled {
  opacity: 0.5;
}
.btn.sec {
  background: var(--color-primary-deep);
  color: var(--color-on-deep);
}
.btn.sec:hover {
  filter: brightness(1.25);
}
.btn.upbtn {
  background: var(--color-primary-deep);
  color: var(--color-on-deep);
}
.btn.upbtn:hover {
  filter: brightness(1.2);
}
.upd {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin: 14px 0 0;
  color: var(--color-muted);
  font-size: 12.5px;
}
.spin {
  animation: rot 0.9s linear infinite;
}
@keyframes rot {
  to {
    transform: rotate(360deg);
  }
}
.lic {
  margin: 22px 0 0;
  color: var(--color-faint);
  font-size: 11.5px;
  line-height: 1.6;
}
</style>