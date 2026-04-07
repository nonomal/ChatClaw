<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { computed, ref, useAttrs } from 'vue'
import { useVModel } from '@vueuse/core'
import { cn } from '@/lib/utils'

const props = defineProps<{
  defaultValue?: string | number
  modelValue?: string | number
  class?: HTMLAttributes['class']
}>()

const emits = defineEmits<{
  (e: 'update:modelValue', payload: string | number): void
}>()

const attrs = useAttrs()

const modelValue = useVModel(props, 'modelValue', emits, {
  passive: true,
  defaultValue: props.defaultValue,
})

const isNumberInput = computed(() => attrs.type === 'number')

/** Skip sanitizing during IME composition; finalize on compositionend. */
const numberComposing = ref(false)

const numberDisplay = computed(() => {
  const v = modelValue.value
  if (v === undefined || v === null) return ''
  return String(v)
})

function parseAttrNumber(raw: unknown): number | undefined {
  if (raw === undefined || raw === null || raw === '') return undefined
  const n = Number(raw)
  return Number.isFinite(n) ? n : undefined
}

function stepAllowsDecimal(): boolean {
  const step = attrs.step
  if (step === 'any') return true
  if (step === undefined || step === '') return false
  const n = Number(step)
  return Number.isFinite(n) && !Number.isInteger(n)
}

function sanitizeNumericString(
  raw: string,
  opts: { allowDecimal: boolean; allowNegative: boolean }
): string {
  let out = ''
  for (let i = 0; i < raw.length; i++) {
    const c = raw[i]
    if (c >= '0' && c <= '9') {
      out += c
      continue
    }
    if (opts.allowNegative && i === 0 && c === '-') {
      out += c
      continue
    }
    if (opts.allowDecimal && c === '.' && !out.includes('.')) {
      out += c
      continue
    }
  }
  return out
}

function clampNumericString(s: string, min?: number, max?: number): string {
  if (s === '' || s === '-') return s
  const n = Number(s)
  if (!Number.isFinite(n)) return s
  let v = n
  if (min !== undefined) v = Math.max(min, v)
  if (max !== undefined) v = Math.min(max, v)
  if (v !== n) return Number.isInteger(v) ? String(v) : String(v)
  return s
}

function buildNumberOptions() {
  const min = parseAttrNumber(attrs.min)
  const max = parseAttrNumber(attrs.max)
  const allowNeg = min === undefined || min < 0
  return {
    min,
    max,
    sanitizeOpts: {
      allowDecimal: stepAllowsDecimal(),
      allowNegative: allowNeg,
    } as const,
  }
}

function sanitizeAndClampRaw(raw: string): string {
  const { min, max, sanitizeOpts } = buildNumberOptions()
  let next = sanitizeNumericString(raw, sanitizeOpts)
  next = clampNumericString(next, min, max)
  return next
}

/** Apply sanitized value to the field and v-model; optional caret after DOM write. */
function applyNumberRaw(el: HTMLInputElement, raw: string, caret?: number) {
  const next = sanitizeAndClampRaw(raw)
  if (next !== el.value) el.value = next
  modelValue.value = next
  if (caret !== undefined && document.activeElement === el) {
    const p = Math.min(Math.max(0, caret), next.length)
    requestAnimationFrame(() => {
      try {
        el.setSelectionRange(p, p)
      } catch {
        /* ignore if type does not support selection */
      }
    })
  }
}

function onNumberInput(e: Event) {
  if (numberComposing.value) return
  const el = e.target as HTMLInputElement
  applyNumberRaw(el, el.value)
}

function onNumberCompositionStart() {
  if (!isNumberInput.value) return
  numberComposing.value = true
}

function onNumberCompositionEnd(e: CompositionEvent) {
  if (!isNumberInput.value) return
  numberComposing.value = false
  const el = e.target as HTMLInputElement
  const pos = el.selectionStart ?? undefined
  applyNumberRaw(el, el.value, pos)
}

const numberBeforeInputPassTypes = new Set([
  'deleteContentBackward',
  'deleteContentForward',
  'deleteByComposition',
  'deleteByDrag',
  'deleteByCut',
  'historyUndo',
  'historyRedo',
])

function onNumberBeforeInput(e: Event) {
  if (!isNumberInput.value || numberComposing.value) return
  const ie = e as InputEvent
  if (ie.isComposing) return

  const el = ie.target as HTMLInputElement
  if (el.disabled || el.readOnly) return

  const it = ie.inputType
  if (numberBeforeInputPassTypes.has(it)) return

  if (it === 'insertLineBreak' || it === 'insertParagraph') {
    ie.preventDefault()
    return
  }

  if (it === 'insertFromPaste' || it === 'insertFromDrop') {
    ie.preventDefault()
    return
  }

  if (it !== 'insertText' && it !== 'insertReplacementText' && it !== 'insertCompositionText') {
    return
  }

  const data = ie.data
  if (data == null) return

  const start = el.selectionStart ?? 0
  const end = el.selectionEnd ?? 0
  const tentative = el.value.slice(0, start) + data + el.value.slice(end)
  const next = sanitizeAndClampRaw(tentative)
  if (next === tentative) return

  ie.preventDefault()
  const newCaret = Math.min(start + data.length, next.length)
  applyNumberRaw(el, tentative, newCaret)
}

function onNumberPaste(e: ClipboardEvent) {
  e.preventDefault()
  const el = e.target as HTMLInputElement
  if (el.disabled || el.readOnly) return
  const text = e.clipboardData?.getData('text/plain') ?? ''
  const start = el.selectionStart ?? 0
  const end = el.selectionEnd ?? 0
  const tentative = el.value.slice(0, start) + text + el.value.slice(end)
  const next = sanitizeAndClampRaw(tentative)
  const newCaret = Math.min(start + text.length, next.length)
  applyNumberRaw(el, tentative, newCaret)
}

function onNumberDrop(e: DragEvent) {
  e.preventDefault()
  const el = e.target as HTMLInputElement
  if (el.disabled || el.readOnly) return
  const text = e.dataTransfer?.getData('text/plain') ?? ''
  const start = el.selectionStart ?? 0
  const end = el.selectionEnd ?? 0
  const tentative = el.value.slice(0, start) + text + el.value.slice(end)
  const next = sanitizeAndClampRaw(tentative)
  applyNumberRaw(el, tentative, Math.min(start + text.length, next.length))
}

function onDragOverMaybeNumber(e: DragEvent) {
  if (!isNumberInput.value) return
  e.preventDefault()
}

function onPasteMaybeNumber(e: ClipboardEvent) {
  if (!isNumberInput.value) return
  onNumberPaste(e)
}

function onDropMaybeNumber(e: DragEvent) {
  if (!isNumberInput.value) return
  onNumberDrop(e)
}

function onInput(e: Event) {
  if (isNumberInput.value) {
    onNumberInput(e)
    return
  }
  modelValue.value = (e.target as HTMLInputElement).value
}

const inputClass = computed(() =>
  cn(
    'file:text-foreground placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground dark:bg-input/30 border-input h-9 w-full min-w-0 rounded-md border bg-transparent px-3 py-1 text-base shadow-xs transition-[color,box-shadow] outline-none file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
    'focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]',
    'aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive',
    props.class
  )
)

const inputValue = computed(() => {
  if (isNumberInput.value) return numberDisplay.value
  const v = modelValue.value
  if (v === undefined || v === null) return ''
  return String(v)
})
</script>

<template>
  <input
    :value="inputValue"
    data-slot="input"
    v-bind="$attrs"
    :class="inputClass"
    @input="onInput"
    @beforeinput="onNumberBeforeInput"
    @compositionstart="onNumberCompositionStart"
    @compositionend="onNumberCompositionEnd"
    @dragover="onDragOverMaybeNumber"
    @paste="onPasteMaybeNumber"
    @drop="onDropMaybeNumber"
  />
</template>
