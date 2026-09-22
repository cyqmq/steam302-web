<script setup>
const props = defineProps({
  modelValue: { type: [String, Number], default: '' },
  options: { type: Array, default: () => [] },
  placeholder: { type: String, default: '' },
  disabled: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue', 'change'])

function change(e) {
  const v = e.target.value
  const orig = props.modelValue
  emit('update:modelValue', v)
  emit('change', { value: v, origin: orig, target: e })
}
</script>

<template>
  <span class="csel" :class="{ disabled }">
    <select
      :value="modelValue"
      :disabled="disabled"
      @change="change"
      :aria-label="placeholder || '选择'"
    >
      <option v-if="placeholder" value="" disabled hidden>{{ placeholder }}</option>
      <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
    </select>
    <span class="caret"></span>
  </span>
</template>

<style scoped>
.csel {
  position: relative;
  display: inline-flex;
  align-items: center;
  transition: all 0.12s;
}
.csel.disabled {
  opacity: 0.45;
  pointer-events: none;
}
select {
  appearance: none;
  -webkit-appearance: none;
  background: var(--color-card);
  border: 1px solid var(--color-border);
  color: var(--color-fg);
  border-radius: 6px;
  padding: 7px 32px 7px 11px;
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
}
select:hover {
  background: var(--color-hover);
  border-color: var(--color-line-strong);
}
select:focus {
  outline: none;
  border-color: var(--color-primary);
}
.caret {
  position: absolute;
  right: 11px;
  width: 12px;
  height: 12px;
  pointer-events: none;
  background: url("data:image/svg+xml;charset=utf-8,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23a0a0a0' stroke-width='2.2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E")
    no-repeat center / 12px;
  transition: opacity 0.15s;
}
</style>