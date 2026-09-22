<script setup>
defineProps({
  title: { type: String, required: true },
  icon: { type: [Object, Function], default: null }
})
</script>

<template>
  <section class="setting-card">
    <div class="card-head">
      <component :is="icon" v-if="icon" class="head-ico" :size="18" />
      <h3 class="head-title">{{ title }}</h3>
      <div class="head-act"><slot name="actions" /></div>
    </div>
    <div class="card-body"><slot /></div>
  </section>
</template>

<style scoped>
.setting-card {
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  margin-bottom: 14px;
  overflow: hidden;
  transition: border-color 0.15s;
}
.setting-card:hover {
  border-color: var(--color-line-strong);
}
.card-head {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 13px 16px;
}
.head-ico {
  color: var(--color-primary);
  flex: none;
}
.head-title {
  margin: 0;
  font-size: 14.5px;
  font-weight: 700;
  color: var(--color-strong);
}
.head-act {
  margin-left: auto;
  display: flex;
  gap: 8px;
  align-items: center;
}
.card-body {
  border-top: 1px solid var(--color-border-soft);
}
</style>