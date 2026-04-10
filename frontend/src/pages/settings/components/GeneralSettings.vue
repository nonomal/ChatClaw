<script setup lang="ts">
import { computed, ref, reactive, onMounted, onUnmounted, nextTick } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import type { AcceptableValue } from 'reka-ui'
import { Events } from '@wailsio/runtime'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  useAppStore,
  useOpenClawGatewayStore,
  isOpenClawRuntimeMutatingPhase,
  type Theme,
} from '@/stores'
import { useLocale, SUPPORTED_LOCALES, type Locale } from '@/composables/useLocale'
import * as ToolchainService from '@bindings/chatclaw/internal/services/toolchain/toolchainservice'
import { BrowserService } from '@bindings/chatclaw/internal/services/browser'
import * as OpenClawRuntimeService from '@bindings/chatclaw/internal/openclaw/runtime/openclawruntimeservice'
import { ToolStatus } from '@bindings/chatclaw/internal/services/toolchain/models'
import { RuntimeStatus } from '@bindings/chatclaw/internal/openclaw/runtime/models'
import SettingsCard from './SettingsCard.vue'
import SettingsItem from './SettingsItem.vue'
import TestInstallDialog from './TestInstallDialog.vue'
import ToolchainSettingsCard from './ToolchainSettingsCard.vue'
import { toast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'

const { t } = useI18n()
const appStore = useAppStore()
const gatewayStore = useOpenClawGatewayStore()
const { runtimePhase } = storeToRefs(gatewayStore)
const { locale: currentLocale, switchLocale } = useLocale()
const testInstallOpen = ref(false)

// 语言选项 - 直接用对应语言的名称显示
const languageOptions = computed(() => {
  const labels: Record<Locale, string> = {
    'zh-CN': '简体中文',
    'en-US': 'English',
    'ar-SA': 'العربية',
    'bn-BD': 'বাংলা',
    'de-DE': 'Deutsch',
    'es-ES': 'Español',
    'fr-FR': 'Français',
    'hi-IN': 'हिन्दी',
    'it-IT': 'Italiano',
    'ja-JP': '日本語',
    'ko-KR': '한국어',
    'pt-BR': 'Português',
    'sl-SI': 'Slovenščina',
    'tr-TR': 'Türkçe',
    'vi-VN': 'Tiếng Việt',
    'zh-TW': '繁體中文',
  }
  return SUPPORTED_LOCALES.map((locale) => ({
    value: locale,
    label: labels[locale] || locale,
  }))
})

// 主题选项
const themeOptions = [
  { value: 'light', label: 'settings.themes.light' },
  { value: 'dark', label: 'settings.themes.dark' },
  { value: 'system', label: 'settings.themes.system' },
]

// 当前语言显示文本
const currentLanguageLabel = computed(() => {
  const option = languageOptions.value.find((opt) => opt.value === currentLocale.value)
  return option ? option.label : ''
})

// 当前主题显示文本
const currentThemeLabel = computed(() => {
  const option = themeOptions.find((opt) => opt.value === appStore.theme)
  return option ? t(option.label) : ''
})

// 处理语言切换
const handleLanguageChange = (value: AcceptableValue) => {
  if (typeof value === 'string' && SUPPORTED_LOCALES.includes(value as Locale)) {
    void switchLocale(value as Locale)
  }
}

// 处理主题切换
const handleThemeChange = (value: AcceptableValue) => {
  if (typeof value === 'string') {
    appStore.setTheme(value as Theme)
  }
}

// ---- Toolchain ----

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

interface OpenClawStatus {
  name: string
  installed: boolean
  installed_version: string
  installing: boolean
  runtime_path: string
}
const openclawStatus = ref<OpenClawStatus | null>(null)
const openclawInstallError = ref(false)

/** Install row busy: local OSS install or runtime dir mutation from OpenClaw manager upgrade. */
const openclawExtensionRuntimeBusy = computed(
  () => !!openclawStatus.value?.installing || isOpenClawRuntimeMutatingPhase(runtimePhase.value)
)

const isDevMode = ref(false)

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

// 加载是否为开发模式
const loadDevMode = async () => {
  try {
    isDevMode.value = await ToolchainService.IsDevMode()
  } catch (e) {
    console.error('Failed to load dev mode:', e)
    isDevMode.value = false
  }
}

// 清除卡住的安装状态
const clearInstallingState = async (toolId: string) => {
  try {
    await ToolchainService.ClearInstallingState(toolId)
    await loadToolStatuses()
  } catch (e) {
    console.error('Failed to clear installing state:', e)
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
  // 加载 OpenClaw 运行时状态
  await loadOpenClawStatus()
  // 加载开发模式
  await loadDevMode()
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

const handleInstall = async (toolId: string) => {
  installErrors[toolId] = false
  const existing = toolStatuses[toolId]
  if (existing) {
    toolStatuses[toolId] = new ToolStatus({ ...existing, installing: true })
  }
  try {
    await ToolchainService.InstallTool(toolId)
    // When InstallTool resolves, install is done; refresh from backend so UI updates
    // even if toolchain:status event was delivered in a context that didn't trigger re-render
    await nextTick()
    await loadToolStatuses()
  } catch (e) {
    console.error(`Failed to install ${toolId}:`, e)
    installErrors[toolId] = true
    // 安装失败时需要重新加载状态
    await loadToolStatuses()
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
      // Assign plain object so Vue reactivity tracks the update reliably
      const status = ToolStatus.createFrom(data)
      toolStatuses[data.name] = { ...status }
      installErrors[data.name] = false
      if (!data.installing) {
        delete downloadProgress[data.name]
      }
      await nextTick()
    }
  })
  // 监听下载进度
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
  <div class="flex w-full flex-col gap-4">
    <SettingsCard :title="t('settings.general.title')">
      <!-- 语言设置 -->
      <SettingsItem :label="t('settings.general.language')">
        <Select :model-value="currentLocale" @update:model-value="handleLanguageChange">
          <SelectTrigger class="w-80">
            <SelectValue>{{ currentLanguageLabel }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="option in languageOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </SelectItem>
          </SelectContent>
        </Select>
      </SettingsItem>

      <!-- 主题设置 -->
      <SettingsItem :label="t('settings.general.theme')" :bordered="false">
        <Select :model-value="appStore.theme" @update:model-value="handleThemeChange">
          <SelectTrigger class="w-80">
            <SelectValue>{{ currentThemeLabel }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="option in themeOptions" :key="option.value" :value="option.value">
              {{ t(option.label) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </SettingsItem>
    </SettingsCard>

    <!-- 开发工具 -->
    <ToolchainSettingsCard
      :tool-defs="toolDefs"
      :tool-statuses="toolStatuses"
      :install-errors="installErrors"
      :download-progress="downloadProgress"
      :openclaw-status="openclawStatus"
      :openclaw-install-error="openclawInstallError"
      :openclaw-extension-runtime-busy="openclawExtensionRuntimeBusy"
      :is-dev-mode="isDevMode"
      :show-test-install-button="true"
      :on-open-extension-path="handleOpenExtensionPath"
      :on-install-open-claw="handleInstallOpenClaw"
      :on-install-tool="handleInstall"
      :on-clear-installing-state="clearInstallingState"
      @test-install="testInstallOpen = true"
    />

    <!-- 测试安装对话框 -->
    <TestInstallDialog v-model:open="testInstallOpen" />
  </div>
</template>
