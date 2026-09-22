<script setup>
const props = defineProps({
  modelValue: { type: String, default: '' },
  value: { type: String, required: true },
  label: { type: String, required: true },
  disabled: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue'])
</script>

<template>
  <label class="cr" :class="{ on: modelValue === value }" :aria-disabled="disabled">
    <input
      type="radio"
      :value="value"
      :checked="modelValue === value"
      :disabled="disabled"
      @change="emit('update:modelValue', value)"
    />
    <span class="dot"></span>
    <span class="txt">{{ label }}</span>
  </label>
</template>

<style scoped>
.cr {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 5px 12px;
  border-radius: 999px;
  border: 1px solid var(--color-border);
  color: var(--color-muted);
  cursor: pointer;
  font-size: 12.5px;
  transition: all 0.15s;
  white-space: nowrap;
}
.cr:hover {
  background: var(--color-hover);
  color: var(--color-strong);
}
.cr[aria-disabled='true'] {
  opacity: 0.45;
  pointer-events: none;
}
.cr.on {
  border-color: var(--color-primary);
  color: var(--color-strong);
  background: var(--color-primary-dim);
}
input {
  appearance: none;
  position: absolute;
  width: 0;
  opacity: 0;
  margin: 0;
}
.dot {
  width: 15px;
  height: 15px;
  border-radius: 50%;
  border: 1.5px solid var(--color-line-strong);
  background: transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
}
.dot::after {
  content: '';
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--color-primary);
  transform: scale(0);
  transition: transform 0.15s;
}
.cr.on .dot {
  border-color: var(--color-primary);
}
.cr.on .dot::after {
  transform: scale(1);
}
.txt {
  line-height: 1;
}
</style>