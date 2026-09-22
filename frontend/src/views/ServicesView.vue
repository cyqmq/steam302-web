<script setup>
import { ref, computed, inject, watch } from 'vue'
import { Pencil, ListPlus, CheckSquare, Square, RefreshCw, Power, Server } from 'lucide-vue-next'
import CustomCheckbox from '../components/CustomCheckbox.vue'
import EditorModal from '../components/EditorModal.vue'
import { post } from '../lib/api.js'
import { store, toast } from '../lib/state.js'

const reload = inject('reload', async () => {})

const GROUP_META = {
  steam: { label: 'Steam', icon: '🎮' },
  game: { label: '游戏', icon: '🎲' },
  image: { label: '图片', icon: '🖼️' },
  chat: { label: '聊天', icon: '💬' },
  media: { label: '媒体', icon: '🎬' },
  misc: { label: '其他', icon: '📦' }
}

const groups = computed(() => {
  const order = []
  const map = {}
  for (const r of store.rules) {
    const g = r.group || 'misc'
    if (!map[g]) {
      map[g] = []
      order.push(g)
    }
    map[g].push(r)
  }
  return order.map((g) => ({ key: g, label: (GROUP_META[g] || { label: g }).label, rules: map[g] }))
})
const total = computed(() => store.rules.length)
const enabled = computed(() => store.rules.filter((r) => r.enabled).length)
const groupAllOn = (grp) => grp.rules.every((r) => r.enabled)
const groupAnyOn = (grp) => grp.rules.some((r) => r.enabled)

const st = computed(() => store.status || {})

const editor = ref(null)
const openEditor = (id) => (editor.value = { mode: 'rule', id })
const openBlacklist = () => (editor.value = { mode: 'blacklist' })

async function toggleRule(rule, v) {
  try {
    await post('/api/rules/' + rule.id, { enabled: v })
    toast((v ? '已启用 ' : '已停用 ') + rule.id)
    reload()
  } catch (e) {
    toast('操作失败: ' + e.message, 'err')
  }
}

async function groupToggle(grp) {
  const target = !groupAllOn(grp)
  try {
    await Promise.all(grp.rules.map((r) => post('/api/rules/' + r.id, { enabled: target })))
    toast('已' + (target ? '启用' : '停用') + ' ' + grp.label + ' 分组')
    reload()
  } catch (e) {
    toast('操作失败: ' + e.message, 'err')
  }
}

async function bulkAll(on) {
  try {
    await post('/api/rules/bulk', { enabled: on })
    toast(on ? '已全部启用' : '已全部停用')
    reload()
  } catch (e) {
    toast('操作失败: ' + e.message, 'err')
  }
}

async function stopAll() {
  try {
    const d = await post('/api/services/stop', {})
    toast('已停止服务: ' + (d?.stopped || []).join(', ') || '（无运行中服务）')
    reload()
  } catch (e) {
    toast('停止失败: ' + e.message, 'err')
  }
}

async function applyAll() {
  try {
    const d = await post('/api/apply', {})
    if (d && d.applied) {
      toast('已重新生成并应用配置')
    } else {
      toast((d && (d.error || d.hint)) || '应用失败', 'err')
    }
    reload()
  } catch (e) {
    toast('应用失败: ' + e.message, 'err')
  }
}

const vitals = computed(() => {
  const s = st.value
  const up = (s.upstream && s.upstream.length) || 0
  const svcs = s.services || {}
  return [
    { k: '本地监听端口', v: `${s.https_port || 25584} / ${s.http_port || 24196}` },
    { k: '绑定 IP', v: s.bind_ip || '127.0.0.1' },
    { k: 'hosts 主机数', v: String(s.host_count ?? '-') },
    { k: 'CDN 优选', v: s.cdn_prefer ? `开启 · ${s.cdn_pinned || 0} 条` : '关闭' },
    { k: '上游域名', v: up ? `共 ${up} 个` : '-' },
    { k: 'DNS 重定向', v: s.dns_redirect ? '开启' : '关闭' },
    { k: '系统代理', v: s.system_proxy || '不处理' },
    { k: 'caddy', v: svcs.caddy || '-', chip: true },
    { k: 'fwd', v: svcs.fwd || '-', chip: true },
    { k: 'dnsd', v: svcs.dnsd || '-', chip: true },
    { k: 'webui', v: svcs.webui || '-', chip: true },
    { k: '更新时间', v: s.timestamp || '' }
  ]
})

watch(
  () => st.value.timestamp,
  () => {}
)
</script>

<template>
  <div class="svc-grid">
    <div class="col-left">
      <div class="toolbar">
        <div class="stat"><b>{{ enabled }}</b><span>/{{ total }} 规则已启用</span></div>
        <div class="ops">
          <button class="btn ghostb" @click="bulkAll(true)"><CheckSquare :size="14" /> 全部启用</button>
          <button class="btn ghostb" @click="bulkAll(false)"><Square :size="14" /> 全部停用</button>
          <button class="btn ghostb" @click="openBlacklist()"><ListPlus :size="14" /> 域名黑名单</button>
        </div>
      </div>

      <div v-if="store.rules.length === 0" class="empty">正在读取规则…</div>

      <section v-for="grp in groups" :key="grp.key" class="grp">
        <div class="grp-head">
          <CustomCheckbox
            :model-value="groupAllOn(grp)"
            :label="'' + (GROUP_META[grp.key] || {}).emoji + ' ' + grp.label"
            @change="groupToggle(grp)"
          />
          <span class="cnt">{{ grp.rules.filter((r) => r.enabled).length }}/{{ grp.rules.length }}</span>
        </div>
        <div
          v-for="ru in grp.rules"
          :key="ru.id"
          class="rule"
          :class="{ off: !ru.enabled }"
        >
          <div class="line">
            <CustomCheckbox :model-value="ru.enabled" @change="(v) => toggleRule(ru, v)" />
            <div class="rl">
              <div class="rn">{{ ru.name || ru.id }}</div>
              <div class="rd">{{ ru.description }}</div>
            </div>
          </div>
          <span class="rid">{{ ru.id }}</span>
          <span class="lamp" :class="{ on: ru.enabled }"></span>
          <button class="ibtn" title="编辑规则 JSON" @click="openEditor(ru.id)"><Pencil :size="14" /></button>
        </div>
      </section>
    </div>

    <aside class="col-right">
      <div class="ctl">
        <h4>服务控制</h4>
        <div class="ctl-ops">
          <button class="btn" :disabled="!st.services || st.services.fwd !== 'active'" @click="applyAll()">
            <RefreshCw :size="14" /> 重载配置
          </button>
          <button class="btn sec" @click="stopAll()"><Power :size="14" /> 停止全部</button>
        </div>
        <div v-if="st.system_proxy" class="hint">检测到系统代理：{{ st.system_proxy }}</div>
      </div>

      <div class="vitals">
        <h4>网络监听 / 设置</h4>
        <div v-for="v in vitals" :key="v.k" class="vi">
          <span class="vk">{{ v.k }}</span>
          <span class="vv" :class="{ chip }" :style="chip && (v.v === 'active' ? 'color:var(--color-on)' : v.v === 'inactive' ? 'color:var(--color-faint)' : '')">{{ v.v }}</span>
        </div>
      </div>
    </aside>

    <EditorModal v-if="editor" :mode="editor.mode" :id="editor.id" @close="editor = null" @saved="reload" />
  </div>
</template>

<style scoped>
.svc-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 280px;
  gap: 18px;
  align-items: start;
}
.col-right {
  position: sticky;
  top: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}
.stat {
  font-size: 13px;
  color: var(--color-muted);
}
.stat b {
  color: var(--color-primary-hi);
  font-size: 18px;
  margin-right: 2px;
}
.ops {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.grp {
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 10px;
  margin-bottom: 12px;
  overflow: hidden;
  transition: border-color 0.15s;
}
.grp:hover {
  border-color: #4a4142;
}
.grp-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  background: var(--color-bg-soft);
  font-weight: 700;
  color: #fff;
  font-size: 13px;
}
.cnt {
  color: var(--color-muted);
  font-weight: 400;
  font-size: 12px;
}
.rule {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 9px 14px;
  border-top: 1px solid var(--color-border-soft);
  transition: background 0.12s;
}
.rule:hover {
  background: rgba(255, 255, 255, 0.035);
}
.rule.off .rn {
  color: var(--color-faint);
}
.line {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 10px;
}
.rl {
  min-width: 0;
}
.rn {
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.rd {
  color: var(--color-muted);
  font-size: 11.5px;
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.rid {
  color: var(--color-faint);
  font-size: 11px;
  font-family: ui-monospace, Consolas, monospace;
  flex: none;
}
.lamp {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--color-faint);
  flex: none;
}
.lamp.on {
  background: var(--color-on);
}
.ibtn {
  width: 26px;
  height: 26px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--color-muted);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.12s;
  flex: none;
}
.ibtn:hover {
  color: #fff;
  background: var(--color-primary-deep);
}
.ctl,
.vitals {
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 10px;
  padding: 12px 14px;
}
.ctl h4,
.vitals h4 {
  margin: 0 0 10px;
  font-size: 12.5px;
  color: var(--color-muted);
  font-weight: 700;
  letter-spacing: 0.5px;
}
.ctl-ops {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.hint {
  margin-top: 10px;
  font-size: 12px;
  color: var(--color-off);
}
.vi {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  padding: 5px 0;
  border-top: 1px dashed var(--color-border-soft);
  font-size: 12.5px;
}
.vi:first-of-type {
  border-top: 0;
}
.vk {
  color: var(--color-muted);
}
.vv {
  color: var(--color-fg);
  font-family: ui-monospace, Consolas, monospace;
  text-align: right;
}
.btn {
  border: 0;
  border-radius: 8px;
  background: linear-gradient(135deg, var(--color-primary-hi), var(--color-primary));
  color: #fff;
  font-weight: 600;
  font-size: 13px;
  padding: 8px 14px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  cursor: pointer;
  transition: all 0.12s;
}
.btn:hover {
  filter: brightness(1.1);
}
.btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.btn.sec {
  background: var(--color-primary-deep);
  color: #f0a9af;
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
</style>