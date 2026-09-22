<script setup>
const props = defineProps({
  modelValue: { type: Boolean, default: false },
  label: { type: String, default: '' },
  disabled: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue', 'change'])

function change(e) {
  const v = e.target.checked
  emit('update:modelValue', v)
  emit('change', v)
}
</script>

<template>
  <label class="cc" :aria-disabled="disabled">
    <input
      type="checkbox"
      :checked="modelValue"
      :disabled="disabled"
      @change="change"
    />
    <span class="sqr"><span class="tick">✓</span></span>
    <span v-if="label" class="txt">{{ label }}</span>
  </label>
</template>

<style scoped>
.cc {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  cursor: pointer;
  user-select: none;
  font-size: 12.5px;
  color: var(--color-fg);
}
.cc:hover {
  color: var(--color-strong);
}
.cc[aria-disabled='true'] {
  opacity: 0.45;
  pointer-events: none;
}
input {
  appearance: none;
  position: absolute;
  width: 0;
  opacity: 0;
  margin: 0;
}
.sqr {
  width: 17px;
  height: 17px;
  border: 1.5px solid var(--color-line-strong);
  border-radius: 4px;
  background: var(--color-card);
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
  flex: none;
}
.tick {
  color: var(--on-accent);
  font-size: 12px;
  font-weight: 800;
  opacity: 0;
  transform: scale(0.5);
  transition: all 0.12s;
}
.cc:hover .sqr {
  border-color: var(--color-primary);
}
input:checked ~ .sqr {
  background: var(--color-primary);
  border-color: var(--color-primary);
}
input:checked ~ .sqr .tick {
  opacity: 1;
  transform: scale(1);
}
.txt {
  line-height: 1;
}
</style>