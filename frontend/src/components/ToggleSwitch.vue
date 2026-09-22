<script setup>
const props = defineProps({
  modelValue: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue', 'change'])

function click() {
  if (props.disabled) return
  const v = !props.modelValue
  emit('update:modelValue', v)
  emit('change', v)
}
</script>

<template>
  <button
    type="button"
    role="switch"
    class="ts"
    :class="{ on: modelValue, off: !modelValue }"
    :disabled="disabled"
    :aria-checked="modelValue"
    @click="click"
  >
    <span class="knob"></span>
  </button>
</template>

<style scoped>
.ts {
  appearance: none;
  width: 40px;
  height: 21px;
  border: 0;
  border-radius: 999px;
  background: #3d3538;
  position: relative;
  cursor: pointer;
  transition: background 0.18s;
  flex: none;
  padding: 0;
}
.ts:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.knob {
  position: absolute;
  top: 2.5px;
  left: 2.5px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.4);
  transition: transform 0.18s;
}
.ts.on {
  background: var(--color-primary);
}
.ts.on .knob {
  transform: translateX(19px);
}
</style>