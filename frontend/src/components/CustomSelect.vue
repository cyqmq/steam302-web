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
  background: #201c1d;
  border: 1px solid var(--color-border);
  color: #fff;
  border-radius: 8px;
  padding: 7px 30px 7px 11px;
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
}
select:hover {
  background: #262224;
  border-color: #4a4142;
}
select:focus {
  outline: none;
  border-color: var(--color-primary);
}
.caret {
  position: absolute;
  right: 11px;
  width: 10px;
  height: 6px;
  pointer-events: none;
  border-left: 5px solid transparent;
  border-right: 5px solid transparent;
  border-top: 6px solid var(--color-muted);
  transition: border-color 0.15s;
}
</style>