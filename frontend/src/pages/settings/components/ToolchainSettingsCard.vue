<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { Download, Check, Loader2, Package, FolderOpen, Play } from 'lucide-vue-next'
import SettingsCard from './SettingsCard.vue'

interface ToolDef {
  id: string
  nameKey: string
  descKey: string
}

interface DownloadProgress {
  tool: string
  url: string
  totalSize: number
  downloaded: number
  percent: number
  speed: number
  elapsedTime: number
  remaining: number
}

interface OpenClawStatus {
  installed: boolean
  installed_version: string
  installing: boolean
  runtime_path: string
}

interface ToolStatusLike {
  installed?: boolean
  installing?: boolean
  has_update?: boolean
  latest_version?: string
  bin_path?: string
}

const props = withDefaults(
  defineProps<{
    toolDefs: ToolDef[]
    toolStatuses: Record<string, ToolStatusLike>
    installErrors: Record<string, boolean>
    downloadProgress: Record<string, DownloadProgress>
    openclawStatus: OpenClawStatus | null
    openclawInstallError: boolean
    openclawExtensionRuntimeBusy: boolean
    showTitle?: boolean
    isDevMode?: boolean
    showTestInstallButton?: boolean
    onOpenExtensionPath: (pathStr: string | undefined) => void | Promise<void>
    onInstallOpenClaw: () => void | Promise<void>
    onInstallTool: (toolId: string) => void | Promise<void>
    onClearInstallingState?: (toolId: string) => void | Promise<void>
  }>(),
  {
    showTitle: true,
    isDevMode: false,
    showTestInstallButton: false,
  }
)

const emit = defineEmits<{
  (e: 'test-install'): void
}>()

const { t } = useI18n()

const hasTestInstallButton = computed(() => props.isDevMode && props.showTestInstallButton)

const formatFileSize = (bytes: number): string => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

const formatSpeed = (kbPerSec: number): string => {
  if (kbPerSec >= 1024) {
    return (kbPerSec / 1024).toFixed(1) + ' MB/s'
  }
  return kbPerSec.toFixed(1) + ' KB/s'
}

const formatRemaining = (ms: number): string => {
  if (ms <= 0) return ''
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const secs = seconds % 60
  return `${minutes}m ${secs}s`
}
</script>

<template>
  <SettingsCard :title="showTitle ? t('settings.general.toolchain.title') : undefined">
    <div class="flex items-start gap-4 border-b border-border p-4 dark:border-white/10">
      <div
        class="flex size-9 shrink-0 items-center justify-center rounded-lg border border-border bg-muted/50 text-muted-foreground dark:border-white/10 dark:bg-white/5"
      >
        <Package class="size-4" />
      </div>
      <div class="min-w-0 flex-1 pt-0.5">
        <span class="text-sm font-medium text-foreground">{{
          t('settings.general.toolchain.openclaw.name')
        }}</span>
        <p class="text-xs text-muted-foreground truncate">
          {{ t('settings.general.toolchain.openclaw.description') }}
        </p>
        <button
          v-if="openclawStatus?.runtime_path"
          type="button"
          class="mt-1 flex min-w-0 max-w-full items-center gap-1 rounded-sm text-left text-xs text-muted-foreground/70 truncate hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          :title="t('settings.general.toolchain.openPathHint')"
          @click="onOpenExtensionPath(openclawStatus.runtime_path)"
        >
          <FolderOpen class="size-3 shrink-0 text-muted-foreground" />
          <span class="truncate">{{ openclawStatus.runtime_path }}</span>
        </button>
        <p
          v-if="openclawStatus?.installed && openclawStatus?.installed_version"
          class="mt-0.5 text-xs text-muted-foreground/60"
          :title="openclawStatus.installed_version"
        >
          {{ t('settings.general.toolchain.testInstall.version') }}:
          {{ openclawStatus.installed_version }}
        </p>
        <div
          v-if="downloadProgress['openclaw'] && openclawStatus?.installing"
          class="mt-2 flex flex-col gap-1"
        >
          <div class="flex items-center justify-between text-xs">
            <span class="text-muted-foreground"
              >{{ downloadProgress['openclaw'].percent.toFixed(1) }}%</span
            >
            <span class="text-muted-foreground">{{
              formatSpeed(downloadProgress['openclaw'].speed)
            }}</span>
          </div>
          <div class="h-1.5 w-full overflow-hidden rounded-full bg-muted">
            <div
              class="h-full bg-primary transition-all duration-300"
              :style="{ width: `${downloadProgress['openclaw'].percent}%` }"
            />
          </div>
          <div class="flex items-center justify-between text-xs text-muted-foreground">
            <span
              >{{ formatFileSize(downloadProgress['openclaw'].downloaded) }} /
              {{ formatFileSize(downloadProgress['openclaw'].totalSize) }}</span
            >
            <span v-if="downloadProgress['openclaw'].remaining > 0">{{
              formatRemaining(downloadProgress['openclaw'].remaining)
            }}</span>
          </div>
        </div>
      </div>

      <div class="flex shrink-0 flex-col items-end gap-2 pt-0.5">
        <template v-if="openclawStatus?.installed && !openclawExtensionRuntimeBusy">
          <span
            class="inline-flex items-center gap-1 whitespace-nowrap rounded-md px-2 py-1 text-xs font-medium text-muted-foreground ring-1 ring-border dark:ring-white/10"
          >
            <Check class="size-3 shrink-0" />
            {{ t('settings.general.toolchain.installed') }}
          </span>
        </template>
        <span
          v-else-if="openclawExtensionRuntimeBusy"
          class="inline-flex items-center gap-1.5 whitespace-nowrap text-xs text-muted-foreground"
        >
          <Loader2 class="size-3 animate-spin" />
          {{
            openclawStatus?.installing
              ? t('settings.general.toolchain.installing')
              : t('settings.openclawRuntime.upgrading')
          }}
        </span>
        <template v-else>
          <span v-if="openclawInstallError" class="text-xs text-destructive">{{
            t('settings.general.toolchain.installFailed')
          }}</span>
          <Button size="sm" variant="outline" @click="onInstallOpenClaw">
            <Download class="size-3.5" />
            {{ t('settings.general.toolchain.install') }}
          </Button>
        </template>
      </div>
    </div>

    <div
      v-for="(tool, index) in toolDefs"
      :key="tool.id"
      class="flex items-start gap-4 p-4"
      :class="index < toolDefs.length - 1 && 'border-b border-border dark:border-white/10'"
    >
      <div
        class="flex size-9 shrink-0 items-center justify-center rounded-lg border border-border bg-muted/50 text-muted-foreground dark:border-white/10 dark:bg-white/5"
      >
        <Package class="size-4" />
      </div>
      <div class="min-w-0 flex-1 pt-0.5">
        <span class="text-sm font-medium text-foreground">{{ t(tool.nameKey) }}</span>
        <p class="text-xs text-muted-foreground truncate">{{ t(tool.descKey) }}</p>
        <button
          v-if="toolStatuses[tool.id]?.bin_path"
          type="button"
          class="mt-1 flex min-w-0 max-w-full items-center gap-1 rounded-sm text-left text-xs text-muted-foreground/70 truncate hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          :title="t('settings.general.toolchain.openPathHint')"
          @click="onOpenExtensionPath(toolStatuses[tool.id]?.bin_path)"
        >
          <FolderOpen class="size-3 shrink-0 text-muted-foreground" />
          <span class="truncate">{{ toolStatuses[tool.id]?.bin_path }}</span>
        </button>
        <div
          v-if="downloadProgress[tool.id] && toolStatuses[tool.id]?.installing"
          class="mt-2 flex flex-col gap-1"
        >
          <div class="flex items-center justify-between text-xs">
            <span class="text-muted-foreground">
              {{ downloadProgress[tool.id].percent.toFixed(1) }}%
            </span>
            <span class="text-muted-foreground">
              {{ formatSpeed(downloadProgress[tool.id].speed) }}
            </span>
          </div>
          <div class="h-1.5 w-full overflow-hidden rounded-full bg-muted">
            <div
              class="h-full bg-primary transition-all duration-300"
              :style="{ width: `${downloadProgress[tool.id].percent}%` }"
            />
          </div>
          <div class="flex items-center justify-between text-xs text-muted-foreground">
            <span>
              {{ formatFileSize(downloadProgress[tool.id].downloaded) }} /
              {{ formatFileSize(downloadProgress[tool.id].totalSize) }}
            </span>
            <span v-if="downloadProgress[tool.id].remaining > 0">
              {{ formatRemaining(downloadProgress[tool.id].remaining) }}
            </span>
          </div>
        </div>
      </div>

      <div class="flex shrink-0 flex-col items-end gap-2 pt-0.5 sm:flex-row sm:items-center">
        <template v-if="toolStatuses[tool.id]?.installed && !toolStatuses[tool.id]?.installing">
          <div class="flex flex-col items-end gap-2">
            <span
              class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-muted-foreground ring-1 ring-border dark:ring-white/10"
            >
              <Check class="size-3" />
              {{ t('settings.general.toolchain.installed') }}
            </span>
            <template v-if="toolStatuses[tool.id]?.has_update">
              <p
                v-if="toolStatuses[tool.id]?.latest_version"
                class="max-w-[200px] text-right text-xs text-muted-foreground"
              >
                {{
                  t('settings.general.toolchain.newVersionHint', {
                    version: toolStatuses[tool.id]?.latest_version,
                  })
                }}
              </p>
              <Button size="sm" variant="outline" @click="onInstallTool(tool.id)">
                <Download class="size-3.5" />
                {{ t('settings.general.toolchain.update') }}
              </Button>
            </template>
          </div>
        </template>
        <span
          v-else-if="toolStatuses[tool.id]?.installing"
          class="inline-flex items-center gap-1.5 text-xs text-muted-foreground"
        >
          <Loader2 class="size-3 animate-spin" />
          {{ t('settings.general.toolchain.installing') }}
          <Button
            v-if="isDevMode"
            size="sm"
            variant="ghost"
            class="ml-1 h-5 px-1 text-xs text-muted-foreground hover:text-destructive"
            @click="onClearInstallingState?.(tool.id)"
          >
            {{ t('settings.general.toolchain.clearState') }}
          </Button>
        </span>
        <template v-else>
          <span v-if="installErrors[tool.id]" class="text-xs text-destructive">
            {{ t('settings.general.toolchain.installFailed') }}
          </span>
          <Button size="sm" variant="outline" @click="onInstallTool(tool.id)">
            <Download class="size-3.5" />
            {{ t('settings.general.toolchain.install') }}
          </Button>
        </template>
      </div>
    </div>

    <div
      v-if="hasTestInstallButton"
      class="flex justify-end border-t border-border p-4 dark:border-white/10"
    >
      <Button variant="outline" size="sm" @click="emit('test-install')">
        <Play class="mr-1 size-3.5" />
        {{ t('settings.general.toolchain.testInstall.button') }}
      </Button>
    </div>
  </SettingsCard>
</template>
