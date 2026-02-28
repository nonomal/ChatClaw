<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { LoaderCircle, FolderOpen } from 'lucide-vue-next'
import { Dialogs } from '@wailsio/runtime'
import type { AcceptableValue } from 'reka-ui'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Button } from '@/components/ui/button'
import { toast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'
import { useAppStore } from '@/stores'
import { SettingsService } from '@bindings/chatclaw/internal/services/settings'
import SettingsCard from './SettingsCard.vue'
import SettingsItem from './SettingsItem.vue'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const saving = ref(false)

const sandboxMode = ref('codex')
const workDir = ref('')
const defaultWorkDir = ref('')

const sandboxOptions = [
  { value: 'codex', labelKey: 'settings.workspace.modeCodex' },
  { value: 'native', labelKey: 'settings.workspace.modeNative' },
]

const currentModeLabel = computed(() => {
  const option = sandboxOptions.find((opt) => opt.value === sandboxMode.value)
  return option ? t(option.labelKey) : ''
})

const displayWorkDir = computed(() => {
  return workDir.value || defaultWorkDir.value || '~/.chatclaw'
})

const loadData = async () => {
  loading.value = true
  try {
    const [modeSetting, dirSetting] = await Promise.all([
      SettingsService.Get('workspace_sandbox_mode'),
      SettingsService.Get('workspace_work_dir'),
    ])

    sandboxMode.value = modeSetting?.value || 'codex'
    workDir.value = dirSetting?.value || ''

    const home = await getHomeDir()
    if (home) {
      defaultWorkDir.value = home + '/.chatclaw'
    }
  } catch (error) {
    console.error('Failed to load workspace settings:', error)
  } finally {
    loading.value = false
  }
}

async function getHomeDir(): Promise<string> {
  try {
    const setting = await SettingsService.Get('workspace_work_dir')
    if (setting?.value) return ''
    return ''
  } catch {
    return ''
  }
}

onMounted(() => {
  loadData()
})

const handleModeChange = (value: AcceptableValue) => {
  if (typeof value === 'string') {
    sandboxMode.value = value
  }
}

const handleSelectDir = async () => {
  if (!appStore.isGUIMode) return
  try {
    const result = await Dialogs.OpenFile({
      Title: t('settings.workspace.selectDir'),
      CanChooseFiles: false,
      CanChooseDirectories: true,
      AllowsMultipleSelection: false,
    })
    if (result && typeof result === 'string') {
      workDir.value = result
    } else if (Array.isArray(result) && result.length > 0) {
      workDir.value = result[0]
    }
  } catch (error) {
    console.error('Failed to select directory:', error)
  }
}

const handleSave = async () => {
  if (saving.value) return
  saving.value = true

  try {
    await Promise.all([
      SettingsService.SetValue('workspace_sandbox_mode', sandboxMode.value),
      SettingsService.SetValue('workspace_work_dir', workDir.value),
    ])
    toast.success(t('settings.workspace.saved'))
  } catch (error) {
    console.error('Failed to save workspace settings:', error)
    toast.error(getErrorMessage(error) || t('settings.workspace.saveFailed'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <SettingsCard :title="t('settings.workspace.title')">
    <div v-if="loading" class="flex items-center justify-center py-8">
      <LoaderCircle class="size-6 animate-spin text-muted-foreground" />
    </div>

    <template v-else>
      <!-- Sandbox Mode -->
      <SettingsItem :label="t('settings.workspace.sandboxMode')">
        <Select :model-value="sandboxMode" @update:model-value="handleModeChange">
          <SelectTrigger class="w-80">
            <SelectValue>{{ currentModeLabel }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="option in sandboxOptions" :key="option.value" :value="option.value">
              {{ t(option.labelKey) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </SettingsItem>

      <!-- Sandbox Mode Description -->
      <div class="px-4 pb-3">
        <p v-if="sandboxMode === 'codex'" class="text-xs text-muted-foreground">
          {{ t('settings.workspace.codexDesc') }}
        </p>
        <p v-else class="text-xs text-muted-foreground">
          {{ t('settings.workspace.nativeDesc') }}
        </p>
      </div>

      <!-- Working Directory -->
      <SettingsItem :label="t('settings.workspace.workDir')">
        <div class="flex items-center gap-2">
          <span class="max-w-56 truncate text-sm text-muted-foreground" :title="displayWorkDir">
            {{ displayWorkDir }}
          </span>
          <Button
            v-if="appStore.isGUIMode"
            variant="outline"
            size="sm"
            class="shrink-0"
            @click="handleSelectDir"
          >
            <FolderOpen class="mr-1.5 size-3.5" />
            {{ t('settings.workspace.changeDir') }}
          </Button>
        </div>
      </SettingsItem>

      <!-- Working Directory Description -->
      <div class="px-4 pb-3">
        <p class="text-xs text-muted-foreground">
          {{ t('settings.workspace.workDirDesc') }}
        </p>
      </div>

      <!-- Save Button -->
      <div class="flex justify-end border-t border-border p-4 dark:border-white/10">
        <Button :disabled="saving" @click="handleSave">
          <LoaderCircle v-if="saving" class="mr-2 size-4 animate-spin" />
          {{ t('common.save') }}
        </Button>
      </div>
    </template>
  </SettingsCard>
</template>
