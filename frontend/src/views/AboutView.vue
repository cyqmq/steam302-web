<script setup>
import { ref } from 'vue'
import { Repeat, RefreshCw, ExternalLink } from 'lucide-vue-next'
import { store, toast } from '../lib/state.js'
import { loadVersion } from '../lib/data.js'

const busy = ref(false)

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
      <p class="ds">steamcommunity 302 网页管理端 — 纯前端 Vue3 复刻原版 SukiUI 深红风格</p>

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
        <button class="btn" :disabled="busy" @click="check">
          <RefreshCw :size="14" :class="{ spin: busy }" /> 检查更新
        </button>
        <button class="btn sec" @click="home">
          <ExternalLink :size="14" /> 项目主页
        </button>
      </div>

      <p class="lic">
        该项目使用 MIT 许可证，界面风格致敬 Avalonia · SukiUI 的「Steamcommunity 302」桌面版。
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
  border-radius: 14px;
  padding: 34px 36px;
  text-align: center;
}
.mark {
  width: 60px;
  height: 60px;
  margin: 0 auto 16px;
  border-radius: 16px;
  background: linear-gradient(135deg, var(--color-primary-hi), var(--color-primary));
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
}
.nm {
  margin: 0;
  color: #fff;
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
  color: #fff;
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
  color: #f0a9af;
}
.btn.sec:hover {
  filter: brightness(1.25);
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