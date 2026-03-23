<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ScheduledTasksService } from '@bindings/chatclaw/internal/services/scheduledtasks'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import type { ScheduledTask, ScheduledTaskRun, ScheduledTaskRunDetail } from '../types'
import { formatDuration, formatTaskTime } from '../utils'
import TaskRunStatusBadge from './TaskRunStatusBadge.vue'
import EmbeddedAssistantPage from '@/pages/assistant/components/EmbeddedAssistantPage.vue'

const props = defineProps<{
  open: boolean
  task: ScheduledTask | null
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
}>()

const loading = ref(false)
const runs = ref<ScheduledTaskRun[]>([])
const selectedRunId = ref<number | null>(null)
const selectedDetail = ref<ScheduledTaskRunDetail | null>(null)
const latestDetailRequestId = ref(0)
const { t } = useI18n()

function displayRunStatusLabel(status: string) {
  // Keep explicit mapping so badge copy stays aligned with i18n keys.
  // 保持显式状态映射，确保徽标文案与国际化 key 一一对应。
  if (status === 'running') return t('scheduledTasks.statusRunning')
  if (status === 'failed') return t('scheduledTasks.statusFailed')
  if (status === 'success') return t('scheduledTasks.statusSuccess')
  return t('scheduledTasks.statusPending')
}

function displayRunTriggerLabel(triggerType: string) {
  // Only known trigger types are localized; unknown values fall back to raw text for safety.
  // 仅对已知触发类型做翻译；未知值回退原文，避免语义被误改。
  if (triggerType === 'schedule') return t('scheduledTasks.runTriggerSchedule')
  if (triggerType === 'manual') return t('scheduledTasks.runTriggerManual')
  return triggerType
}

watch(
  () => props.open,
  async (value) => {
    if (!value) {
      // Closing the dialog must invalidate in-flight detail requests and clear stale UI state.
      // 关闭弹窗时必须废弃进行中的详情请求，并清空旧的界面状态。
      latestDetailRequestId.value += 1
      runs.value = []
      selectedRunId.value = null
      selectedDetail.value = null
      return
    }
    if (!props.task) return
    loading.value = true
    const currentTaskId = props.task.id
    latestDetailRequestId.value += 1
    selectedDetail.value = null
    try {
      runs.value = await ScheduledTasksService.ListScheduledTaskRuns(props.task.id, 1, 50)
      if (!props.open || props.task?.id !== currentTaskId) return
      selectedRunId.value = runs.value[0]?.id ?? null
      if (selectedRunId.value) {
        await loadRunDetail(selectedRunId.value)
      } else {
        selectedDetail.value = null
      }
    } finally {
      loading.value = false
    }
  },
  { immediate: true }
)

async function loadRunDetail(runId: number) {
  const requestId = latestDetailRequestId.value + 1
  latestDetailRequestId.value = requestId
  const currentTaskId = props.task?.id ?? null
  const detail = await ScheduledTasksService.GetScheduledTaskRunDetail(runId)

  // Only the latest click should win, so slow old requests cannot override the new selection.
  // 只接受最后一次点击对应的结果，避免旧请求慢返回时覆盖新选择。
  if (requestId !== latestDetailRequestId.value) return
  if (!props.open) return
  if ((props.task?.id ?? null) !== currentTaskId) return
  if (selectedRunId.value !== runId) return
  selectedDetail.value = detail
}

async function selectRun(run: ScheduledTaskRun) {
  // Clear the previous conversation immediately so the embedded assistant never boots with stale props.
  // 先清空上一次会话，避免内嵌助手先用旧参数启动再被异步覆盖。
  selectedRunId.value = run.id
  selectedDetail.value = null
  await loadRunDetail(run.id)
}
</script>

<template>
  <Dialog :open="open" @update:open="(value) => emit('update:open', value)">
    <DialogContent class="max-h-[90vh] overflow-hidden sm:!w-auto sm:min-w-[1000px] sm:!max-w-[1760px]">
      <DialogHeader>
        <DialogTitle>{{ task?.name }} / {{ t('scheduledTasks.runHistoryTitle') }}</DialogTitle>
      </DialogHeader>

      <div class="flex h-[70vh] min-h-0 gap-4">
        <div
          class="shrink-0 overflow-y-auto overflow-x-hidden rounded-lg border border-border sm:w-[248px]"
        >
          <div v-if="loading" class="p-4 text-sm text-muted-foreground">{{ t('common.loading') }}</div>
          <div v-else-if="runs.length === 0" class="p-4 text-sm text-muted-foreground">
            {{ t('scheduledTasks.noRuns') }}
          </div>
          <button
            v-for="run in runs"
            :key="run.id"
            class="w-full border-b border-border px-3 py-3 text-left transition-colors hover:bg-accent/40"
            :class="selectedRunId === run.id ? 'bg-accent/50' : ''"
            @click="selectRun(run)"
          >
            <div class="flex items-start gap-2 overflow-hidden">
              <TaskRunStatusBadge
                class="mt-0.5 shrink-0"
                :status="run.status"
                :label="displayRunStatusLabel(run.status)"
              />
              <div class="flex min-w-0 flex-1 flex-col items-start gap-1 text-left">
                <div class="w-full text-[11px] leading-4 text-foreground">
                  {{ formatTaskTime(run.started_at) }}
                </div>
                <div class="flex w-full items-center gap-1 text-[11px] text-muted-foreground">
                  <span class="shrink-0">{{ displayRunTriggerLabel(run.trigger_type) }}</span>
                  <span class="shrink-0 text-muted-foreground/50">&middot;</span>
                  <span class="truncate">{{ formatDuration(run.duration_ms) }}</span>
                </div>
              </div>
            </div>

            <TooltipProvider>
              <Tooltip v-if="run.error_message">
                <TooltipTrigger as-child>
                  <div class="mt-2 line-clamp-1 text-xs text-red-600">{{ run.error_message }}</div>
                </TooltipTrigger>
                <TooltipContent>
                  <p class="max-w-sm whitespace-pre-wrap text-xs">{{ run.error_message }}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </button>
        </div>

        <div class="min-h-0 flex-1 overflow-hidden rounded-lg border border-border">
          <div
            v-if="!selectedDetail?.conversation?.id"
            class="flex h-full items-center justify-center text-sm text-muted-foreground"
          >
            {{ t('scheduledTasks.conversationEmpty') }}
          </div>
          <EmbeddedAssistantPage
            v-else
            :key="`${selectedRunId ?? 'no-run'}-${selectedDetail.conversation.id}`"
            :conversation-id="selectedDetail.conversation.id"
            :agent-id="selectedDetail.conversation.agent_id"
            :run-id="selectedRunId"
          />
        </div>
      </div>
    </DialogContent>
  </Dialog>
</template>
