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
import { toast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'
import ToolchainSettingsCard from './ToolchainSettingsCard.vue'

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

const handlePauseOpenClawInstall = async () => {
  try {
    await ToolchainService.AbortDownload('openclaw')
    if (openclawStatus.value) {
      openclawStatus.value = { ...openclawStatus.value, installing: false }
    }
    delete downloadProgress['openclaw']
    toast.default(t('settings.runtimeEnvironment.paused'))
  } catch (e) {
    console.error('Failed to pause openclaw runtime install:', e)
    toast.error(getErrorMessage(e) || t('settings.runtimeEnvironment.pauseFailed'))
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
  navigationStore.navigateToModule('assistant')
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
          @click="handlePauseOpenClawInstall"
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

    <ToolchainSettingsCard
      :tool-defs="toolDefs"
      :tool-statuses="toolStatuses"
      :install-errors="installErrors"
      :download-progress="downloadProgress"
      :openclaw-status="openclawStatus"
      :openclaw-install-error="openclawInstallError"
      :openclaw-extension-runtime-busy="openclawExtensionRuntimeBusy"
      :show-title="false"
      :on-open-extension-path="handleOpenExtensionPath"
      :on-install-open-claw="handleInstallOpenClaw"
      :on-install-tool="handleInstall"
    />
  </div>
</template>
