<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import IconAgentAdd from '@/assets/icons/agent-add.svg'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { toast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'
import LogoIcon from '@/assets/images/logo.svg'
import IconSettings from '@/assets/icons/settings.svg'
import CreateAgentDialog from './components/CreateAgentDialog.vue'
import AgentSettingsDialog from './components/AgentSettingsDialog.vue'
import { AgentsService, type Agent } from '@bindings/willchat/internal/services/agents'
import { Events } from '@wailsio/runtime'
import { Send } from 'lucide-vue-next'

type ListMode = 'personal' | 'team'

const { t } = useI18n()

const mode = ref<ListMode>('personal')

const agents = ref<Agent[]>([])
const activeAgentId = ref<number | null>(null)

const createOpen = ref(false)
const settingsOpen = ref(false)
const settingsAgent = ref<Agent | null>(null)
const loading = ref(false)

// Mock input for text selection feature
const inputText = ref('')

const activeAgent = computed(() => {
  if (activeAgentId.value == null) return null
  return agents.value.find((a) => a.id === activeAgentId.value) ?? null
})

const loadAgents = async () => {
  loading.value = true
  try {
    const list = await AgentsService.ListAgents()
    agents.value = list
    if (activeAgentId.value == null && list.length > 0) {
      activeAgentId.value = list[0].id
    }
  } catch (error: unknown) {
    toast.error(getErrorMessage(error) || t('assistant.errors.loadFailed'))
  } finally {
    loading.value = false
  }
}

const handleCreate = async (data: { name: string; prompt: string; icon: string }) => {
  loading.value = true
  try {
    const created = await AgentsService.CreateAgent({
      name: data.name,
      prompt: data.prompt,
      icon: data.icon,
    })
    if (!created) {
      throw new Error(t('assistant.errors.createFailed'))
    }
    createOpen.value = false
    agents.value = [created, ...agents.value]
    activeAgentId.value = created.id
    toast.success(t('assistant.toasts.created'))
  } catch (error: unknown) {
    toast.error(getErrorMessage(error) || t('assistant.errors.createFailed'))
  } finally {
    loading.value = false
  }
}

const openSettings = (agent: Agent) => {
  settingsAgent.value = agent
  settingsOpen.value = true
}

const handleUpdated = (updated: Agent) => {
  const idx = agents.value.findIndex((a) => a.id === updated.id)
  if (idx >= 0) agents.value[idx] = updated
  if (activeAgentId.value === updated.id) activeAgentId.value = updated.id
}

const handleDeleted = (id: number) => {
  agents.value = agents.value.filter((a) => a.id !== id)
  if (activeAgentId.value === id) {
    activeAgentId.value = agents.value.length > 0 ? agents.value[0].id : null
  }
}

// Mock send function (placeholder for future implementation)
const handleSend = () => {
  if (!inputText.value.trim()) return
  toast.default(`发送消息: ${inputText.value.slice(0, 50)}...`)
  inputText.value = ''
}

// Listen for text selection events
let unsubscribeTextSelection: (() => void) | null = null

onMounted(() => {
  loadAgents()

  // Listen for text selection to send to assistant
  unsubscribeTextSelection = Events.On('text-selection:send-to-assistant', (event: any) => {
    const payload = Array.isArray(event?.data) ? event.data[0] : event?.data ?? event
    const text = payload?.text ?? ''
    if (text) {
      inputText.value = text
      // Auto-send after a short delay
      setTimeout(() => {
        handleSend()
      }, 100)
    }
  })
})

onUnmounted(() => {
  unsubscribeTextSelection?.()
  unsubscribeTextSelection = null
})
</script>

<template>
  <div class="flex h-full w-full overflow-hidden bg-background">
    <!-- 左侧：助手列表 -->
    <aside class="flex w-sidebar shrink-0 flex-col border-r border-border">
      <div class="flex items-center justify-between gap-2 p-3">
        <div class="inline-flex rounded-md bg-muted p-1">
          <button
            :class="
              cn(
                'rounded px-3 py-1 text-sm transition-colors',
                mode === 'personal'
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              )
            "
            @click="mode = 'personal'"
          >
            {{ t('assistant.modes.personal') }}
          </button>
          <button
            :class="
              cn('rounded px-3 py-1 text-sm transition-colors', 'cursor-not-allowed opacity-50')
            "
            disabled
          >
            {{ t('assistant.modes.team') }}
          </button>
        </div>

        <Button size="icon" variant="ghost" :disabled="loading" @click="createOpen = true">
          <IconAgentAdd class="size-4 text-muted-foreground" />
        </Button>
      </div>

      <div class="flex-1 overflow-auto px-2 pb-3">
        <div
          v-if="agents.length === 0"
          class="mx-2 mt-2 flex items-center justify-center rounded-lg border border-border bg-card p-4 text-sm text-muted-foreground"
        >
          <div class="text-center text-sm text-muted-foreground">
            {{ t('assistant.empty') }}
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <button
            v-for="a in agents"
            :key="a.id"
            :class="
              cn(
                'group flex h-11 w-full items-center gap-2 rounded px-2 text-left outline-none transition-colors',
                a.id === activeAgentId
                  ? 'bg-zinc-100 text-foreground dark:bg-accent'
                  : 'bg-white text-muted-foreground shadow-[0px_1px_4px_0px_rgba(0,0,0,0.1)] hover:bg-accent/50 hover:text-foreground dark:bg-zinc-800/50 dark:shadow-[0px_1px_4px_0px_rgba(255,255,255,0.05)]'
              )
            "
            @click="activeAgentId = a.id"
          >
          <div
            class="flex size-8 shrink-0 items-center justify-center overflow-hidden rounded-[10px] border border-border bg-white text-foreground dark:border-white/15 dark:bg-white/5"
          >
            <img v-if="a.icon" :src="a.icon" class="size-6 object-contain" />
            <LogoIcon v-else class="size-6 opacity-90" />
          </div>

          <div class="min-w-0 flex-1">
            <div class="truncate text-sm font-normal">
              {{ a.name }}
            </div>
          </div>

          <!-- 设置按钮：默认隐藏，悬停显示 -->
          <Button
            size="icon"
            variant="ghost"
            class="opacity-0 group-hover:opacity-100"
            :title="t('assistant.actions.settings')"
            @click.stop="openSettings(a)"
          >
            <IconSettings class="size-4 opacity-80 group-hover:opacity-100" />
          </Button>
          </button>
        </div>
      </div>
    </aside>

    <!-- 右侧：聊天区（含模拟输入框） -->
    <section class="flex flex-1 flex-col overflow-hidden">
      <div class="flex flex-1 flex-col items-center justify-center gap-2">
        <div class="text-lg font-semibold text-foreground">
          {{ activeAgent?.name ?? t('assistant.placeholders.noAgentSelected') }}
        </div>
        <div class="max-w-dialog-md px-6 text-center text-sm text-muted-foreground">
          {{ t('assistant.placeholders.chatComingSoon') }}
        </div>
      </div>

      <!-- Mock input area for text selection feature -->
      <div class="border-t border-border p-4">
        <div class="mx-auto flex max-w-2xl items-end gap-2">
          <div class="flex-1">
            <textarea
              v-model="inputText"
              class="min-h-[60px] w-full resize-none rounded-lg border border-border bg-background p-3 text-sm outline-none transition-colors focus:border-primary"
              :placeholder="t('winsnap.placeholder')"
              @keydown.enter.exact.prevent="handleSend"
            />
          </div>
          <Button
            size="icon"
            class="mb-1 shrink-0"
            :disabled="!inputText.trim()"
            @click="handleSend"
          >
            <Send class="size-4" />
          </Button>
        </div>
      </div>
    </section>

    <CreateAgentDialog v-model:open="createOpen" :loading="loading" @create="handleCreate" />
    <AgentSettingsDialog
      v-model:open="settingsOpen"
      :agent="settingsAgent"
      @updated="handleUpdated"
      @deleted="handleDeleted"
    />
  </div>
</template>
