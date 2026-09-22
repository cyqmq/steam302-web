<script setup>
import { ref } from 'vue'
import { Search, X, Power, Trash2 } from 'lucide-vue-next'
import CustomRadio from '../components/CustomRadio.vue'

const tab = ref('active')
const q = ref('')
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
        <input class="qinp" type="text" placeholder="搜索转发会话（含主机名、规则、源地址）" v-model="q" />
        <button v-if="q" class="qclr" @click="q = ''"><X :size="13" /></button>
      </div>
      <div class="ops">
        <button class="btn ghostb" disabled><Power :size="13" /> 强制断开</button>
        <button class="btn ghostb" disabled><Power :size="13" /> 断开所有活动会话</button>
        <button class="btn ghostb" disabled><Trash2 :size="13" /> 清空</button>
      </div>
    </div>

    <div class="table">
      <div class="thead">
        <span>主机名</span><span>总下载</span><span>总上传</span><span>下载速度</span>
        <span>上传速度</span><span>匹配规则</span><span>连接时间</span><span>源地址</span>
      </div>
      <div class="unsupported">
        <div class="us-ico"><Power :size="22" /></div>
        <p>当前后端不支持连接监控</p>
        <p class="us-sub">请升级后端到包含连接监控能力的 V15 版本。</p>
        <p class="us-note">实际转发不受影响，此页仅用于展示活动转发会话。</p>
      </div>
    </div>
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
  grid-template-columns: 2.2fr 1fr 1fr 1.2fr 1.2fr 1.4fr 1fr 1.2fr;
  gap: 8px;
  padding: 10px 14px;
  background: var(--color-card-head);
  border-bottom: 1px solid var(--color-border);
  color: var(--color-muted);
  font-size: 11.5px;
  font-weight: 600;
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
.unsupported p {
  margin: 4px 0;
  font-size: 13px;
  color: var(--color-fg);
}
.unsupported .us-sub {
  color: var(--color-faint);
}
.unsupported .us-note {
  color: var(--color-faint);
  font-size: 11.5px;
}
</style>