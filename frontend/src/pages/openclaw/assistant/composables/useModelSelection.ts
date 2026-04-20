import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'
import {
  ProvidersService,
  type ProviderWithModels,
  type Model,
} from '@bindings/chatclaw/internal/services/providers'
import { getBinding as getChatwikiBinding } from '@/lib/chatwikiCache'
import {
  formatModelDisplayLabel,
  getChatwikiAvailabilityStatus,
  getFirstSelectableModelKey,
  hasSelectableModelsForGroup,
  isSelectionAvailable,
} from '@/lib/chatwikiModelAvailability'
import {
  ConversationsService,
  type Conversation,
  UpdateConversationInput,
} from '@bindings/chatclaw/internal/services/conversations'
import type { OpenClawAgent } from '@bindings/chatclaw/internal/openclaw/agents'

export function useModelSelection() {
  const { t } = useI18n()

  const providersWithModels = ref<ProviderWithModels[]>([])
  const selectedModelKey = ref('')
  const chatwikiAvailability = ref<'available' | 'unbound' | 'non_cloud'>('available')

  const getDisplayModelName = (providerId: string, model: Model) =>
    formatModelDisplayLabel(
      providerId,
      model.name?.trim() || model.model_id?.trim() || '-',
      chatwikiAvailability.value
    )

  const hasModels = computed(() => {
    return providersWithModels.value.some((pw) =>
      pw.model_groups.some((g) => g.type === 'llm' && g.models.length > 0)
    )
  })

  const hasSelectableLlmModels = computed(() =>
    hasSelectableModelsForGroup(providersWithModels.value, 'llm', chatwikiAvailability.value)
  )

  const selectedModelInfo = computed(() => {
    if (!selectedModelKey.value) return null
    const [providerId, modelId] = selectedModelKey.value.split('::')
    if (!providerId || !modelId) return null
    for (const pw of providersWithModels.value) {
      if (pw.provider.provider_id !== providerId) continue
      for (const group of pw.model_groups) {
        if (group.type !== 'llm') continue
        const model = group.models.find((m) => m.model_id === modelId)
        if (model) {
          return {
            providerId,
            modelId,
            modelName: getDisplayModelName(providerId, model),
            capabilities: model.capabilities,
          }
        }
      }
    }
    return null
  })

  const loadModels = async () => {
    try {
      const [providers, binding] = await Promise.all([
        ProvidersService.ListProviders(),
        getChatwikiBinding().catch(() => null),
      ])
      chatwikiAvailability.value = getChatwikiAvailabilityStatus(binding)
      const enabled = providers.filter((p) => p.enabled)
      // Load provider models in parallel; allow partial failures.
      const settled = await Promise.allSettled(
        enabled.map((p) => ProvidersService.GetProviderWithModels(p.provider_id))
      )
      const ok: ProviderWithModels[] = []
      let failedCount = 0
      for (const s of settled) {
        if (s.status === 'fulfilled') {
          if (s.value) ok.push(s.value)
        } else {
          failedCount += 1
          console.warn('Failed to load provider models:', s.reason)
        }
      }
      // Sort free providers to the end so user-configured (stronger) models come first.
      ok.sort((a, b) => {
        const aFree = Boolean((a.provider as { is_free?: boolean }).is_free)
        const bFree = Boolean((b.provider as { is_free?: boolean }).is_free)
        if (aFree === bFree) return 0
        return aFree ? 1 : -1
      })
      providersWithModels.value = ok

      // If some providers failed but we still have models, keep UI usable and show a gentle hint.
      if (failedCount > 0 && ok.length > 0) {
        toast.default(t('assistant.errors.loadModelsPartialFailed'))
      } else if (failedCount > 0 && ok.length === 0) {
        toast.error(t('assistant.errors.loadModelsFailed'))
      }
    } catch (error: unknown) {
      toast.error(getErrorMessage(error) || t('assistant.errors.loadModelsFailed'))
    }
  }

  const selectDefaultModel = (
    activeAgent: OpenClawAgent | null,
    activeConversation: Conversation | null
  ) => {
    if (!activeAgent) {
      selectedModelKey.value = ''
      return
    }

    {
      const conv = activeConversation
      if (conv?.llm_provider_id && conv?.llm_model_id) {
        const key = `${conv.llm_provider_id}::${conv.llm_model_id}`
        if (
          isSelectionAvailable(providersWithModels.value, key, 'llm', chatwikiAvailability.value)
        ) {
          selectedModelKey.value = key
          return
        }
      }
    }

    const agentProviderId = activeAgent.default_llm_provider_id
    const agentModelId = activeAgent.default_llm_model_id

    if (agentProviderId && agentModelId) {
      const key = `${agentProviderId}::${agentModelId}`
      if (isSelectionAvailable(providersWithModels.value, key, 'llm', chatwikiAvailability.value)) {
        selectedModelKey.value = key
        return
      }
    }

    const defaultUseModelKey = getFirstDefaultUseModelKey(
      providersWithModels.value,
      chatwikiAvailability.value
    )
    if (defaultUseModelKey) {
      selectedModelKey.value = defaultUseModelKey
      return
    }

    selectedModelKey.value = getFirstSelectableModelKey(
      providersWithModels.value,
      'llm',
      chatwikiAvailability.value
    )
  }

  const getFirstDefaultUseModelKey = (
    providers: ProviderWithModels[],
    status: 'available' | 'unbound' | 'non_cloud'
  ): string => {
    for (const pw of providers) {
      const providerId = pw.provider.provider_id
      if (!pw.provider.enabled) continue
      if (providerId === 'chatwiki' && status !== 'available') continue
      for (const group of pw.model_groups) {
        if (group.type !== 'llm') continue
        const model = group.models.find(
          (m) => m.enabled !== false && String(m.default_use_model ?? '0') === '1'
        )
        if (model) {
          return `${providerId}::${model.model_id}`
        }
      }
    }
    return ''
  }

  const parseSelectedModelKey = (key: string): { providerId: string; modelId: string } | null => {
    if (!key) return null
    const [providerId, modelId] = key.split('::')
    if (!providerId || !modelId) return null
    return { providerId, modelId }
  }

  const saveModelToConversationIfNeeded = async (
    activeConversationId: number | null,
    activeConversation: Conversation | null,
    opts?: { silent?: boolean }
  ) => {
    const silent = opts?.silent ?? true
    if (!activeConversationId) return

    const parsed = parseSelectedModelKey(selectedModelKey.value)
    if (!parsed) return

    // Avoid redundant updates when switching conversations (we already read from DB into selectedModelKey).
    const current = activeConversation
    if (
      current &&
      current.llm_provider_id === parsed.providerId &&
      current.llm_model_id === parsed.modelId
    ) {
      return
    }

    try {
      const updated = await ConversationsService.UpdateConversation(
        activeConversationId,
        new UpdateConversationInput({
          llm_provider_id: parsed.providerId,
          llm_model_id: parsed.modelId,
        })
      )
      return updated
    } catch (error: unknown) {
      // Non-critical: if this fails, backend will continue using the previously saved model.
      if (!silent) {
        toast.error(getErrorMessage(error) || t('assistant.errors.updateConversationFailed'))
      } else {
        console.warn('Failed to save model to conversation:', error)
      }
      return null
    }
  }

  return {
    providersWithModels,
    selectedModelKey,
    hasModels,
    hasSelectableLlmModels,
    selectedModelInfo,
    loadModels,
    selectDefaultModel,
    saveModelToConversationIfNeeded,
  }
}
