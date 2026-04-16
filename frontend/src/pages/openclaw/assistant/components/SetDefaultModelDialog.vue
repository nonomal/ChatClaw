<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { ProviderIcon } from '@/components/ui/provider-icon'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
} from '@/components/ui/select'
import {
  ProvidersService,
  type ProviderWithModels,
} from '@bindings/chatclaw/internal/services/providers'
import type { OpenClawAgent } from '@bindings/chatclaw/internal/openclaw/agents'

export interface SetDefaultModelResult {
  defaultLlMProviderId: string
  defaultLlMModelId: string
}

const props = defineProps<{
  open: boolean
  agent: OpenClawAgent | null
  loading?: boolean
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  confirm: [data: SetDefaultModelResult]
}>()

const { t } = useI18n()

const providersWithModels = ref<ProviderWithModels[]>([])
const modelProviderId = ref('')
const modelId = ref('')
const modelName = ref('')
const modelKey = ref('')

const hasDefaultModel = computed(() => modelProviderId.value !== '' && modelId.value !== '')
const isValid = computed(() => hasDefaultModel.value)

const loadModels = async () => {
  try {
    const providers = await ProvidersService.ListProviders()
    const enabled = providers.filter((p) => p.enabled)
    const results: ProviderWithModels[] = []
    for (const p of enabled) {
      try {
        const withModels = await ProvidersService.GetProviderWithModels(p.provider_id)
        if (withModels) results.push(withModels)
      } catch (error: unknown) {
        console.warn(`Failed to load provider models (${p.provider_id}):`, error)
      }
    }
    providersWithModels.value = results

    if (modelProviderId.value && modelId.value) {
      for (const pw of results) {
        if (pw.provider.provider_id !== modelProviderId.value) continue
        for (const group of pw.model_groups) {
          if (group.type !== 'llm') continue
          const m = group.models.find((x) => x.model_id === modelId.value)
          if (m) modelName.value = m.name
        }
      }
    }
  } catch (error: unknown) {
    console.warn('Failed to load models:', error)
  }
}

const onModelKeyChange = (val: any) => {
  if (typeof val !== 'string') return
  modelKey.value = val
  if (!val) {
    modelProviderId.value = ''
    modelId.value = ''
    modelName.value = ''
    return
  }
  const [p, m] = val.split('::')
  modelProviderId.value = p ?? ''
  modelId.value = m ?? ''
  modelName.value = ''
  for (const pw of providersWithModels.value) {
    if (pw.provider.provider_id !== modelProviderId.value) continue
    for (const group of pw.model_groups) {
      if (group.type !== 'llm') continue
      const found = group.models.find((x) => x.model_id === modelId.value)
      if (found) modelName.value = found.name
    }
  }
}

const selectedProviderIsFree = computed(() => {
  if (!modelProviderId.value || !providersWithModels.value.length) return false
  const pw = providersWithModels.value.find(
    (p) => p.provider?.provider_id === modelProviderId.value
  )
  return pw?.provider && Boolean((pw.provider as { is_free?: boolean }).is_free)
})

function isProviderFree(pw: ProviderWithModels | undefined): boolean {
  if (!pw?.provider) return false
  const p = pw.provider as { is_free?: boolean }
  return Boolean(p.is_free)
}

watch(
  () => props.open,
  (open) => {
    if (!open) return
    modelProviderId.value = ''
    modelId.value = ''
    modelName.value = ''
    modelKey.value = ''
    void loadModels()
  }
)

const handleClose = () => emit('update:open', false)

const handleConfirm = () => {
  if (!isValid.value || props.loading) return
  emit('confirm', {
    defaultLlMProviderId: modelProviderId.value,
    defaultLlMModelId: modelId.value,
  })
}
</script>

<template>
  <Dialog :open="open" @update:open="handleClose">
    <DialogContent size="lg">
      <DialogHeader>
        <DialogTitle>{{ t('assistant.settings.model.setDefaultModelTitle') }}</DialogTitle>
      </DialogHeader>

      <div class="flex flex-col gap-4 py-4">
        <p class="text-sm text-muted-foreground">
          {{ t('assistant.settings.model.setDefaultModelDesc', { name: agent?.name ?? '' }) }}
        </p>

        <div class="flex flex-col gap-1.5">
          <label class="text-sm font-medium text-foreground">
            {{ t('assistant.settings.model.defaultModel') }}
            <span class="text-destructive">*</span>
          </label>
          <Select :model-value="modelKey" @update:model-value="onModelKeyChange">
            <SelectTrigger class="h-9 w-full rounded-md border border-border bg-background">
              <div v-if="hasDefaultModel" class="flex min-w-0 items-center gap-2">
                <ProviderIcon
                  :icon="modelProviderId"
                  :size="16"
                  class="text-foreground"
                />
                <div class="min-w-0 truncate text-sm font-medium text-foreground">
                  {{ modelName || modelId }}
                </div>
                <span
                  v-if="selectedProviderIsFree"
                  class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground ring-1 ring-border"
                >
                  {{ t('assistant.chat.freeBadge') }}
                </span>
              </div>
              <div v-else class="text-sm text-muted-foreground">
                {{ t('assistant.settings.model.noDefaultModel') }}
              </div>
            </SelectTrigger>
            <SelectContent class="max-h-[260px]">
              <SelectGroup>
                <SelectLabel>{{
                  t('assistant.settings.model.defaultModel')
                }}</SelectLabel>
                <template
                  v-for="pw in providersWithModels"
                  :key="pw.provider.provider_id"
                >
                  <SelectLabel class="mt-2 flex items-center gap-1.5">
                    <span>{{ pw.provider.name }}</span>
                    <span
                      v-if="isProviderFree(pw)"
                      class="rounded px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground ring-1 ring-border"
                    >
                      {{ t('assistant.chat.freeBadge') }}
                    </span>
                  </SelectLabel>
                  <template v-for="g in pw.model_groups" :key="g.type">
                    <template v-if="g.type === 'llm'">
                      <SelectItem
                        v-for="m in g.models"
                        :key="pw.provider.provider_id + '::' + m.model_id"
                        :value="pw.provider.provider_id + '::' + m.model_id"
                      >
                        {{ m.name }}
                      </SelectItem>
                    </template>
                  </template>
                </template>
              </SelectGroup>
            </SelectContent>
          </Select>
          <p class="text-xs text-muted-foreground">
            {{ t('assistant.settings.model.defaultModelHint') }}
          </p>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" :disabled="loading" @click="handleClose">
          {{ t('assistant.actions.cancel') }}
        </Button>
        <Button :disabled="!isValid || loading" @click="handleConfirm">
          {{ t('assistant.actions.confirm') }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
