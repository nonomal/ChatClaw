<script setup lang="ts">
import { reactive, ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { Events } from '@wailsio/runtime'
import { Button } from '@/components/ui/button'
import {
  useNavigationStore,
  useOpenClawGatewayStore,
  isOpenClawRuntimeMutatingPhase,
} from '@/stores'
import * as ToolchainService from '@bindings/chatclaw/internal/services/toolchain/toolchainservice'
import * as OpenClawRuntimeService from '@bindings/chatclaw/internal/openclaw/runtime/openclawruntimeservice'
import { BrowserService } from '@bindings/chatclaw/internal/services/browser'
import { RuntimeStatus } from '@bindings/chatclaw/internal/openclaw/runtime/models'
import { ToolStatus } from '@bindings/chatclaw/internal/services/toolchain/models'
import { Download, Check, Loader2, Package, FolderOpen } from 'lucide-vue-next'
import { toast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'
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
  name: string
  installed: boolean
  installed_version: string
  installing: boolean
  runtime_path: string
}

const { t } = useI18n()
const navigationStore = useNavigationStore()
const gatewayStore = useOpenClawGatewayStore()
const { runtimePhase } = storeToRefs(gatewayStore)

const toolDefs: ToolDef[] = [
  {
    id: 'uv',
    nameKey: 'settings.general.toolchain.uv.name',
    descKey: 'settings.general.toolchain.uv.description',
  },
  {
    id: 'bun',
    nameKey: 'settings.general.toolchain.bun.name',
    descKey: 'settings.general.toolchain.bun.description',
  },
  {
    id: 'codex',
    nameKey: 'settings.general.toolchain.codex.name',
    descKey: 'settings.general.toolchain.codex.description',
  },
]

const toolStatuses = reactive<Record<string, ToolStatus>>({})
const installErrors = reactive<Record<string, boolean>>({})
const downloadProgress = reactive<Record<string, DownloadProgress>>({})
const openclawStatus = ref<OpenClawStatus | null>(null)
const openclawInstallError = ref(false)

const openclawExtensionRuntimeBusy = computed(
  () => !!openclawStatus.value?.installing || isOpenClawRuntimeMutatingPhase(runtimePhase.value)
)

const runtimeReady = computed(() => !!openclawStatus.value?.installed)

const loadOpenClawStatus = async () => {
  try {
    const status = await ToolchainService.GetOpenClawRuntimeStatus()
    if (status) {
      openclawStatus.value = {
        name: status.name,
        installed: status.installed,
        installed_version: status.installed_version,
        installing: status.installing,
        runtime_path: status.runtime_path,
      }
      openclawInstallError.value = false
    }
  } catch (e) {
    console.error('Failed to load openclaw status:', e)
    openclawStatus.value = {
      name: 'openclaw',
      installed: false,
      installed_version: '',
      installing: false,
      runtime_path: '',
    }
  }
}

const loadToolStatuses = async () => {
  try {
    const statuses = await ToolchainService.GetAllToolStatus()
    for (const s of statuses) {
      toolStatuses[s.name] = s
    }
  } catch (e) {
    console.error('Failed to load toolchain statuses:', e)
  }
  await loadOpenClawStatus()
}

const handleInstallOpenClaw = async () => {
  if (isOpenClawRuntimeMutatingPhase(runtimePhase.value) && !openclawStatus.value?.installing) {
    return
  }
  openclawInstallError.value = false
  if (openclawStatus.value) {
    openclawStatus.value = { ...openclawStatus.value, installing: true }
  }
  try {
    await OpenClawRuntimeService.InstallAndStartRuntime()
    await nextTick()
    await loadOpenClawStatus()
  } catch (e) {
    console.error('Failed to install openclaw runtime:', e)
    openclawInstallError.value = true
    if (openclawStatus.value) {
      openclawStatus.value = { ...openclawStatus.value, installing: false }
    }
  }
}

const handleInstall = async (toolId: string) => {
  installErrors[toolId] = false
  const existing = toolStatuses[toolId]
  if (existing) {
    toolStatuses[toolId] = new ToolStatus({ ...existing, installing: true })
  }
  try {
    await ToolchainService.InstallTool(toolId)
    await nextTick()
    await loadToolStatuses()
  } catch (e) {
    console.error(`Failed to install ${toolId}:`, e)
    installErrors[toolId] = true
    await loadToolStatuses()
  }
}

const handlePauseGateway = async () => {
  try {
    await OpenClawRuntimeService.StopGateway()
    toast.success(t('settings.openclawRuntime.stopSuccess'))
  } catch (e) {
    toast.error(getErrorMessage(e) || t('settings.openclawRuntime.stopFailed'))
  }
}

const handleStartUsing = () => {
  navigationStore.navigateToModule('openclaw-dashboard')
}

const handleOpenExtensionPath = async (pathStr: string | undefined) => {
  const p = pathStr?.trim()
  if (!p) return
  try {
    await BrowserService.OpenPathInFileManager(p)
  } catch (e) {
    console.error('Failed to open path in file manager:', e)
    toast.error(getErrorMessage(e) || t('settings.general.toolchain.openPathFailed'))
  }
}

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

let unsubscribeToolchain: (() => void) | null = null
let unsubscribeProgress: (() => void) | null = null
let unsubscribeOpenClawStatus: (() => void) | null = null

onMounted(() => {
  void loadToolStatuses()
  void gatewayStore.poll()
  unsubscribeOpenClawStatus = Events.On('openclaw:status', (event: any) => {
    const data = event?.data?.[0] ?? event?.data ?? event
    if (data) {
      gatewayStore.ingestRuntimeStatus(RuntimeStatus.createFrom(data))
    }
  })
  unsubscribeToolchain = Events.On('toolchain:status', async (event: any) => {
    const data = event?.data?.[0] ?? event?.data ?? event
    if (data && data.name) {
      const status = ToolStatus.createFrom(data)
      toolStatuses[data.name] = { ...status }
      installErrors[data.name] = false
      if (!data.installing) {
        delete downloadProgress[data.name]
      }
      await nextTick()
    }
  })
  unsubscribeProgress = Events.On('toolchain:download-progress', (event: any) => {
    const data = event?.data?.[0] ?? event?.data ?? event
    if (data && data.tool) {
      downloadProgress[data.tool] = data
    }
  })
})

onUnmounted(() => {
  unsubscribeToolchain?.()
  unsubscribeToolchain = null
  unsubscribeProgress?.()
  unsubscribeProgress = null
  unsubscribeOpenClawStatus?.()
  unsubscribeOpenClawStatus = null
})
</script>

<template>
  <div class="mx-auto flex w-full max-w-[920px] flex-col gap-6 pt-6">
    <section class="flex flex-col items-center gap-3 pt-2 text-center">
      <h2 class="text-3xl font-semibold text-foreground">
        {{ t('settings.runtimeEnvironment.title') }}
      </h2>
      <p class="text-lg text-foreground/85">
        {{ t('settings.runtimeEnvironment.subtitle') }}
      </p>
      <div class="flex flex-wrap items-center justify-center gap-3">
        <Button
          v-if="!runtimeReady"
          variant="outline"
          class="min-w-32"
          :disabled="openclawExtensionRuntimeBusy"
          @click="handleInstallOpenClaw"
        >
          {{ openclawExtensionRuntimeBusy ? t('settings.general.toolchain.installing') : t('settings.runtimeEnvironment.installNow') }}
        </Button>
        <Button
          v-if="!runtimeReady"
          variant="secondary"
          class="min-w-32"
          :disabled="!openclawExtensionRuntimeBusy"
          @click="toast.default(t('common.close'))"
        >
          {{ t('settings.runtimeEnvironment.pause') }}
        </Button>
        <Button v-if="runtimeReady" variant="outline" class="min-w-32" @click="handlePauseGateway">
          {{ t('settings.runtimeEnvironment.pause') }}
        </Button>
        <Button v-if="runtimeReady" variant="secondary" class="min-w-32" @click="handleStartUsing">
          {{ t('settings.runtimeEnvironment.startUsing') }}
        </Button>
      </div>
      <p class="text-sm text-muted-foreground">
        <template v-if="runtimeReady">
          {{ t('settings.runtimeEnvironment.installedHint') }}
          <span class="text-red-500">{{ t('settings.runtimeEnvironment.managerText') }}</span>
          {{ t('settings.runtimeEnvironment.managerSuffix') }}
        </template>
        <template v-else>
          {{ t('settings.runtimeEnvironment.notInstalledHint') }}
        </template>
      </p>
    </section>

    <SettingsCard>
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
            @click="handleOpenExtensionPath(openclawStatus.runtime_path)"
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
            <Button size="sm" variant="outline" @click="handleInstallOpenClaw">
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
            @click="handleOpenExtensionPath(toolStatuses[tool.id]?.bin_path)"
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
                <Button size="sm" variant="outline" @click="handleInstall(tool.id)">
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
          </span>
          <template v-else>
            <span v-if="installErrors[tool.id]" class="text-xs text-destructive">
              {{ t('settings.general.toolchain.installFailed') }}
            </span>
            <Button size="sm" variant="outline" @click="handleInstall(tool.id)">
              <Download class="size-3.5" />
              {{ t('settings.general.toolchain.install') }}
            </Button>
          </template>
        </div>
      </div>
    </SettingsCard>
  </div>
</template>
