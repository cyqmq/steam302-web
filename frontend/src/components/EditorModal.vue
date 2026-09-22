<script setup>
import { ref, onMounted } from 'vue'
import { X, Pencil, ListPlus } from 'lucide-vue-next'
import { get, put } from '../lib/api.js'
import { toast } from '../lib/state.js'

const props = defineProps({
  mode: { type: String, required: true }, // rule | blacklist
  id: { type: String, default: '' }
})
const emit = defineEmits(['close', 'saved'])

const text = ref('')
const busy = ref(false)

const title = props.mode === 'rule' ? `规则编辑器 · ${props.id}` : '域名黑名单'
const placeholder =
  props.mode === 'rule'
    ? '{\n  "id": "...",\n  "name": "...",\n  "enabled": true\n}'
    : '{\n  "domains": ["store.steampowered.com"]\n}'

onMounted(async () => {
  try {
    if (props.mode === 'rule') {
      const d = await get('/api/rule/' + props.id)
      text.value = d.text
    } else {
      const d = await get('/api/blacklist')
      text.value = JSON.stringify({ domains: d.domains || [] }, null, 2)
    }
  } catch (e) {
    toast('加载失败: ' + e.message, 'err')
  }
})

async function save() {
  busy.value = true
  try {
    if (props.mode === 'rule') {
      await put('/api/rule/' + props.id, { text: text.value })
    } else {
      const obj = JSON.parse(text.value)
      if (!Array.isArray(obj.domains)) throw new Error('需为 {"domains": [...]}')
      await put('/api/blacklist', { domains: obj.domains })
    }
    toast('已保存并重新生成配置')
    emit('saved')
    emit('close')
  } catch (e) {
    toast('保存失败: ' + e.message, 'err')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="mask" @click.self="emit('close')">
    <div class="frame">
      <div class="head">
        <span class="ti">{{ title }}</span>
        <button class="x" @click="emit('close')"><X :size="16" /></button>
      </div>
      <textarea
        v-model="text"
        class="ta"
        :placeholder="placeholder"
        spellcheck="false"
      ></textarea>
      <div class="foot">
        <span class="note">修改后立即校验并重新生成代理配置。</span>
        <div class="ops">
          <button class="btn ghostb" @click="emit('close')">取消</button>
          <button class="btn" :disabled="busy" @click="save">
            <component :is="mode === 'rule' ? Pencil : ListPlus" :size="14" />
            {{ busy ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 50;
}
.frame {
  width: min(720px, 92vw);
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 8px;
  overflow: hidden;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--color-border);
}
.ti {
  font-weight: 700;
  color: #fff;
  font-size: 14px;
}
.x {
  border: 0;
  background: transparent;
  color: var(--color-muted);
  cursor: pointer;
  border-radius: 6px;
  padding: 4px;
  display: inline-flex;
}
.x:hover {
  color: #fff;
  background: var(--color-primary-deep);
}
.ta {
  width: 100%;
  height: 380px;
  resize: vertical;
  border: 0;
  background: #171516;
  color: #e8e8e6;
  font: 12.5px/1.55 ui-monospace, Consolas, monospace;
  padding: 14px 16px;
  outline: none;
}
.foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 11px 16px;
  border-top: 1px solid var(--color-border);
}
.note {
  font-size: 11.5px;
  color: var(--color-faint);
}
.ops {
  display: flex;
  gap: 8px;
}
.btn {
  border: 0;
  border-radius: 8px;
  background: linear-gradient(135deg, var(--color-primary-hi), var(--color-primary));
  color: #fff;
  font-weight: 600;
  font-size: 12.5px;
  padding: 7px 14px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
.btn:disabled {
  opacity: 0.5;
}
.btn.ghostb {
  background: transparent;
  border: 1px solid var(--color-border);
  color: var(--color-muted);
}
.btn.ghostb:hover {
  color: #fff;
  border-color: var(--color-primary);
}
</style>