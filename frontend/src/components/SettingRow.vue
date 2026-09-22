<script setup>
defineProps({
  title: { type: String, required: true },
  subtitle: { type: String, default: '' },
  icon: { type: [Object, Function], default: null }
})
</script>

<template>
  <div class="setting-row">
    <div class="row-left">
      <component :is="icon" v-if="icon" class="ricon" :size="16" />
      <div class="rtext">
        <div class="rt">{{ title }}</div>
        <div v-if="subtitle" class="rs">{{ subtitle }}</div>
      </div>
    </div>
    <div class="row-right"><slot /></div>
  </div>
</template>

<style scoped>
.setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  padding: 12px 16px;
  border-top: 1px solid var(--color-border-soft);
  transition: background 0.12s;
}
.setting-row:hover {
  background: rgba(255, 255, 255, 0.035);
}
.row-left {
  display: flex;
  align-items: center;
  gap: 11px;
  min-width: 0;
}
.ricon {
  color: var(--color-faint);
  flex: none;
  transition: color 0.12s;
}
.setting-row:hover .ricon {
  color: var(--color-primary);
}
.rt {
  color: #fff;
  font-weight: 600;
  font-size: 13.5px;
  line-height: 1.35;
}
.rs {
  color: var(--color-muted);
  font-size: 11.5px;
  margin-top: 3px;
  line-height: 1.4;
}
.row-right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: none;
  flex-wrap: wrap;
  justify-content: flex-end;
}
</style>