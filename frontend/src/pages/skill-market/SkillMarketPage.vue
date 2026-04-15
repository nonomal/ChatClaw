<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  RefreshCw,
  Search,
  Loader2,
  FolderOpen,
  Check,
  Download,
  Trash2,
  Grid2X2,
  Package,
  Shield,
  ChevronLeft,
  FileText,
  FileCode,
  User,
  Star,
  SquareDashedMousePointer,
  Plus,
} from 'lucide-vue-next'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import MarkdownRenderer from '@/components/MarkdownRenderer.vue'
import { toast } from '@/components/ui/toast'
import { useToast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'
import { cn } from '@/lib/utils'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Service as SkillMarketService,
  InstallTargetScope,
} from '@bindings/chatclaw/internal/services/skillmarket'
import type {
  Skill,
  SkillCategory,
  InstallTargetConfig,
} from '@bindings/chatclaw/internal/services/skillmarket'
import { BrowserService } from '@bindings/chatclaw/internal/services/browser'
import { OpenClawAgentsService, type OpenClawAgent } from '@bindings/chatclaw/internal/openclaw/agents'
import { OpenClawSkillsService, type OpenClawSkill, type SkillFileInfo } from '@bindings/chatclaw/internal/openclaw/skills'
import { useNavigationStore, useAppStore } from '@/stores'
import { Events } from '@wailsio/runtime'

const props = defineProps<{
  tabId: string
}>()

const { t, locale } = useI18n()
const navigationStore = useNavigationStore()
const appStore = useAppStore()
const { toast: showToast } = useToast()

type PageTab = 'installed' | 'browse'

const activeTab = ref<PageTab>('installed')

const agents = ref<OpenClawAgent[]>([])
const selectedAgentId = ref<number | null>(null)
const agentsLoading = ref(false)

const installTargets = ref<InstallTargetConfig[]>([])
const selectedScope = ref<InstallTargetScope>(InstallTargetScope.ScopeOpenClawShared)
const targetsLoading = ref(false)
const sharedDir = ref('')
const addDialogOpen = ref(false)

const agentWorkspaceTarget = computed(() => {
  if (!selectedAgentId.value) return null
  // Always show the agent workspace target when an agent is selected,
  // regardless of the current scope (shared or agent-workspace).
  return installTargets.value.find((t) => t.openClawAgentId) ?? null
})

const installDialogOpen = ref(false)
const installDialogSkill = ref<Skill | null>(null)
const installDialogLoading = ref(false)

async function loadAgents() {
  agentsLoading.value = true
  try {
    const list = await OpenClawAgentsService.ListAgents()
    agents.value = list
    if (list.length > 0 && selectedAgentId.value === null) {
      selectedAgentId.value = list[0].id
    }
  } catch (error) {
    console.error('Failed to load agents:', error)
  } finally {
    agentsLoading.value = false
  }
}

async function loadInstallTargets() {
  targetsLoading.value = true
  try {
    const scopes = await SkillMarketService.ListAvailableTargets(
      selectedAgentId.value,
      locale.value
    )
    installTargets.value = scopes
  } catch (error) {
    console.error('Failed to load install targets:', error)
  } finally {
    targetsLoading.value = false
  }
}

// Reload targets when agent changes
watch(selectedAgentId, () => {
  loadInstallTargets()
  // Reset scope to openclaw-shared if current is agent-workspace
  if (selectedScope.value.startsWith('agent-workspace:')) {
    selectedScope.value = InstallTargetScope.ScopeOpenClawShared
  }
  loadInstalledSkills()
  loadBrowseSkills(false)
})

const categories = ref<SkillCategory[]>([])
const skills = ref<Skill[]>([])
const totalCount = ref(0)
const totalSkillCount = computed(() => categories.value.reduce((sum, cat) => sum + (cat.skillCount || 0), 0))
const browseLoading = ref(false)
const browsePage = ref(1)
const BROWSE_PAGE_SIZE = 24
const selectedCategoryId = ref<number | null>(null)
const browseListEl = ref<HTMLElement | null>(null)
const searchQuery = ref('')
const browseHasMore = computed(() => skills.value.length < totalCount.value)

// ==================== 已安装技能 (OpenClaw 技能) ====================
const installedSkills = ref<OpenClawSkill[]>([])
const installedLoading = ref(false)
const installedSearchQuery = ref('')
const skillTogglePending = ref<Set<string>>(new Set())

async function toggleSkillEnabled(slug: string, currentEligible: boolean | null | undefined) {
  if (skillTogglePending.value.has(slug)) return
  skillTogglePending.value.add(slug)
  try {
    // Set eligible = !currentEligible (toggle), store to openclaw.json
    const newEnabled = currentEligible !== true // if currently enabled (true/undefined) → disable; if disabled → enable
    await OpenClawSkillsService.SetSkillEnabled(slug, newEnabled)

    // Update local state immediately
    const skill = installedSkills.value.find((s) => s.slug === slug)
    if (skill) {
      skill.eligible = newEnabled
    }
  } catch (err) {
    toast.error(getErrorMessage(err) || t('settings.skillMarket.toggleFailed'))
  } finally {
    skillTogglePending.value.delete(slug)
  }
}

// 基础搜索过滤
const searchedInstalledSkills = computed(() => {
  if (!installedSearchQuery.value.trim()) return installedSkills.value
  const q = installedSearchQuery.value.trim().toLowerCase()
  return installedSkills.value.filter(
    (s) =>
      s.slug.toLowerCase().includes(q) ||
      (s.name && s.name.toLowerCase().includes(q)) ||
      (s.description && s.description.toLowerCase().includes(q))
  )
})

// 安装目录过滤
const scopeFilteredInstalledSkills = computed(() => {
  const isWorkspace = selectedScope.value.startsWith('agent-workspace:')
  const targetAgentId = isWorkspace ? selectedScope.value.replace('agent-workspace:', '') : ''
  return searchedInstalledSkills.value.filter((s) => {
    if (isWorkspace) {
      return s.location === 'workspace' && s.agentId === targetAgentId
    }
    return s.location === 'shared' && s.skillRoot
  })
})

async function loadInstalledSkills() {
  installedLoading.value = true
  try {
    const skills = await OpenClawSkillsService.ListSkills()
    console.log('[SkillMarket] loadInstalledSkills =>', skills.length, 'skills:', skills.map((s) => ({ slug: s.slug, location: s.location, skillRoot: s.skillRoot, agentId: s.agentId, dataSource: s.dataSource })))

    // Read disabled state from openclaw.json
    let disabledMap: Record<string, boolean> = {}
    try {
      disabledMap = await OpenClawSkillsService.GetDisabledSkillSlugs() as Record<string, boolean> ?? {}
    } catch {
      // Gateway may not be ready; disabledMap stays empty (all enabled by default)
    }

    installedSkills.value = skills.map((skill) => ({
      ...skill,
      // eligible = !(disabled in openclaw.json). Undefined/null from gateway → treat as enabled.
      eligible: disabledMap[skill.slug] === true ? false : true,
    }))
  } catch (error) {
    console.error('Failed to load installed skills:', error)
  } finally {
    installedLoading.value = false
  }
}

const installedDetailOpen = ref(false)
const installedDetailSkill = ref<OpenClawSkill | null>(null)
const installedDetailFiles = ref<SkillFileInfo[]>([])
const installedDetailLoading = ref(false)
let installedDetailLoadVersion = 0
let installedFileLoadVersion = 0

const installedDetailName = computed(() =>
  installedDetailSkill.value?.name || installedDetailSkill.value?.slug || ''
)
const installedDetailDescription = computed(() =>
  installedDetailSkill.value?.description || ''
)
const installedDetailVersion = computed(() =>
  installedDetailSkill.value?.version || ''
)
const installedDetailIsBuiltIn = computed(() =>
  installedDetailSkill.value?.dataSource === 'extra'
)

const isBuiltInUninstall = computed(() =>
  installedDetailIsBuiltIn.value || installedSkills.value.some(
    (s) => (s.slug === uninstallTargetName.value || s.name === uninstallTargetName.value) && s.dataSource === 'extra'
  )
)

async function showInstalledDetail(skill: OpenClawSkill) {
  installedDetailSkill.value = skill
  installedDetailFiles.value = []
  selectedFilePath.value = ''
  fileContent.value = ''
  installedDetailLoading.value = true
  installedDetailOpen.value = true

  const version = ++installedDetailLoadVersion
  try {
    const files = await OpenClawSkillsService.ListSkillFiles(skill.skillRoot || skill.slug)
    if (version !== installedDetailLoadVersion) return
    installedDetailFiles.value = files
    if (files.length > 0) {
      await selectFile(files[0].path)
    }
  } catch {
    if (version !== installedDetailLoadVersion) return
  } finally {
    if (version === installedDetailLoadVersion) {
      installedDetailLoading.value = false
    }
  }
}

async function openInstalledSkillDir() {
  if (installedDetailSkill.value?.skillRoot) {
    const dir = installedDetailSkill.value.skillRoot.replace(/[/\\][^/\\]+$/, '')
    await BrowserService.OpenDirectory(dir)
  }
}

// ==================== 远程技能市场 ====================

const detailOpen = ref(false)
const detailSkill = ref<Skill | null>(null)
const selectedFilePath = ref('')
const fileContent = ref('')
const fileLoading = ref(false)
let fileLoadVersion = 0

const detailTitle = computed(() => {
  if (!detailSkill.value) return ''
  return detailSkill.value.name || detailSkill.value.skillName || ''
})

const detailIsInstalled = computed(() => {
  return detailSkill.value?.isBuiltin === true
})

const BINARY_EXTENSIONS = new Set([
  'png', 'jpg', 'jpeg', 'gif', 'webp', 'ico', 'bmp', 'svg',
  'woff', 'woff2', 'ttf', 'otf', 'eot',
  'zip', 'tar', 'gz', 'bz2', 'xz', '7z',
  'exe', 'dll', 'so', 'dylib',
  'pdf', 'doc', 'docx', 'xls', 'xlsx',
  'mp3', 'mp4', 'wav', 'avi', 'mov',
])

function isBinaryFile(path: string): boolean {
  const ext = path.split('.').pop()?.toLowerCase() || ''
  return BINARY_EXTENSIONS.has(ext)
}

function isMarkdownFile(path: string): boolean {
  return /\.md$/i.test(path)
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)}MB`
}

function stripFrontmatter(content: string): string {
  const trimmed = content.trimStart()
  if (!trimmed.startsWith('---')) return content
  const rest = trimmed.slice(3)
  const endIdx = rest.indexOf('\n---')
  if (endIdx === -1) return content
  return rest.slice(endIdx + 4).trimStart()
}

function sourceLabel(source: string): string {
  switch (source) {
    case 'clawhub': return t('settings.skillMarket.sourceClawhub')
    case 'skillhub': return t('settings.skillMarket.sourceSkillhub')
    case 'chatclaw': return t('settings.skillMarket.sourceChatclaw')
    default: return source
  }
}

function scopeLabel(scope: InstallTargetScope): string {
  if (scope === InstallTargetScope.ScopeLocal) return t('settings.skillMarket.targetLocal')
  if (scope === InstallTargetScope.ScopeOpenClawShared) return t('settings.skillMarket.targetOpenclawShared')
  if (scope.startsWith('agent-workspace:')) return t('settings.skillMarket.targetAgentWorkspace')
  return scope
}

const renderedFileContent = computed(() => {
  if (!selectedFilePath.value || isBinaryFile(selectedFilePath.value)) return null
  let content = fileContent.value
  if (isMarkdownFile(selectedFilePath.value)) {
    content = stripFrontmatter(content)
  }
  return content
})

const isSelectedFileMarkdown = computed(() => isMarkdownFile(selectedFilePath.value))

function getCodeLanguage(path: string): string | null {
  const ext = path.split('.').pop()?.toLowerCase()
  const map: Record<string, string> = {
    ts: 'typescript', tsx: 'tsx', js: 'javascript', jsx: 'jsx',
    py: 'python', rb: 'ruby', go: 'go', rs: 'rust', java: 'java',
    c: 'c', cpp: 'cpp', cs: 'csharp', swift: 'swift', kt: 'kotlin',
    sh: 'bash', bash: 'bash', zsh: 'bash', yaml: 'yaml', yml: 'yaml',
    json: 'json', xml: 'xml', html: 'html', css: 'css', scss: 'scss',
    sql: 'sql', md: 'markdown', mdx: 'markdown',
  }
  return ext ? (map[ext] ?? null) : null
}

const fileContentAsMarkdown = computed(() => {
  if (!renderedFileContent.value) return ''
  const lang = getCodeLanguage(selectedFilePath.value) || 'text'
  return '```' + lang + '\n' + renderedFileContent.value + '\n```'
})

function setOpenClawShared() {
  selectedScope.value = InstallTargetScope.ScopeOpenClawShared
  loadInstalledSkills()
  loadBrowseSkills(false)
}

function handleBrowseScroll(e: Event) {
  const el = e.target as HTMLElement
  const threshold = 80
  if (browseHasMore.value && !browseLoading.value) {
    if (el.scrollHeight - el.scrollTop - el.clientHeight < threshold) {
      void loadBrowseSkills(true)
    }
  }
}

async function setAgentWorkspace() {
  if (!selectedAgentId.value) return
  // If targets are still loading, wait for them so we can read the correct openClawAgentId.
  if (targetsLoading.value) {
    await new Promise<void>((resolve) => {
      const stop = watch(targetsLoading, (loading) => {
        if (!loading) {
          stop()
          resolve()
        }
      })
    })
  }
  const target = installTargets.value.find((t) => t.openClawAgentId)
  if (!target) return
  selectedScope.value = `agent-workspace:${target.openClawAgentId}` as InstallTargetScope
  loadInstalledSkills()
  loadBrowseSkills(false)
}

async function loadCategories() {
  try {
    categories.value = await SkillMarketService.ListCategories(locale.value)
  } catch (error) {
    console.error('Failed to load categories:', error)
  }
}

async function loadBrowseSkills(append = false) {
  if (browseLoading.value) return
  browseLoading.value = true
  if (!append) {
    skills.value = []
    browsePage.value = 1
  }
  try {
    const result = await SkillMarketService.ListSkills({
      categoryId: selectedCategoryId.value ?? undefined,
      name: searchQuery.value.trim() || undefined,
      locale: locale.value,
      page: append ? browsePage.value + 1 : 1,
      pageSize: BROWSE_PAGE_SIZE,
      scope: selectedScope.value,
    } as any)
    if (append) {
      skills.value = [...skills.value, ...result[0]]
      browsePage.value++
    } else {
      skills.value = result[0]
    }
    totalCount.value = result[1]
  } catch (error) {
    toast.error(getErrorMessage(error) || t('settings.skillMarket.loadFailed'))
  } finally {
    browseLoading.value = false
  }
}

function loadDetail(skill: Skill) {
  detailSkill.value = skill
}

async function selectFile(path: string) {
  if (path === selectedFilePath.value) return
  selectedFilePath.value = path
  if (isBinaryFile(path)) {
    fileContent.value = ''
    return
  }
  const version = ++fileLoadVersion
  fileLoading.value = true
  try {
    if (installedDetailOpen.value && installedDetailSkill.value?.skillRoot) {
      // Local skill file
      const content = await OpenClawSkillsService.ReadSkillFile(installedDetailSkill.value.skillRoot, path)
      if (version !== fileLoadVersion) return
      fileContent.value = content
    } else {
      // Remote file preview not yet implemented
      fileContent.value = `// ${t('settings.skillMarket.filePreviewNA')} // ${path}`
    }
  } catch {
    if (version !== fileLoadVersion) return
    fileContent.value = ''
  } finally {
    if (version === fileLoadVersion) {
      fileLoading.value = false
    }
  }
}

async function handleRefresh() {
  if (activeTab.value === 'browse') {
    await loadBrowseSkills(false)
  } else {
    await loadInstalledSkills()
  }
}

async function handleSearch() {
  if (activeTab.value === 'browse') {
    await loadBrowseSkills(false)
  }
  // installedSkills are already reactive, no reset needed
}

async function handleCategoryClick(categoryId: number | null) {
  selectedCategoryId.value = categoryId
  await loadBrowseSkills(false)
}

const installingSet = ref(new Set<string>())

function openInstallDialog(skill: Skill) {
  installDialogSkill.value = skill
  installDialogOpen.value = true
}

async function handleInstall() {
  if (!installDialogSkill.value) return
  const skill = installDialogSkill.value
  installDialogLoading.value = true
  try {
    await SkillMarketService.InstallSkill(skill, selectedScope.value)
    toast.success(t('settings.skillMarket.installSuccess'))
    installDialogOpen.value = false
    installDialogSkill.value = null
    await Promise.all([loadInstalledSkills(), loadBrowseSkills(false)])
    const idx = skills.value.findIndex((s) => s.id === skill.id)
    if (idx !== -1) skills.value[idx].isBuiltin = true
  } catch (error) {
    toast.error(getErrorMessage(error) || t('settings.skillMarket.installFailed'))
  } finally {
    installDialogLoading.value = false
  }
}

const uninstallTargetName = ref<string | null>(null)
const uninstallTargetScopeOverride = ref<InstallTargetScope | null>(null)

async function handleUninstall(skillName: string) {
  const scope = uninstallTargetScopeOverride.value ?? selectedScope.value
  uninstallTargetScopeOverride.value = null
  try {
    await SkillMarketService.UninstallSkill(skillName, scope)
    toast.success(t('settings.skillMarket.uninstallSuccess'))
    installedSkills.value = installedSkills.value.filter((s) => s.slug !== skillName && s.name !== skillName)
    skills.value.forEach((s) => {
      if (s.skillName === skillName) s.isBuiltin = false
    })
    if (installedDetailSkill.value && (installedDetailSkill.value.slug === skillName || installedDetailSkill.value.name === skillName)) {
      installedDetailOpen.value = false
    }
    if (detailSkill.value?.skillName === skillName) {
      detailSkill.value = { ...detailSkill.value, isBuiltin: false }
    }
  } catch (error) {
    toast.error(getErrorMessage(error) || t('settings.skillMarket.uninstallFailed'))
  } finally {
    uninstallTargetName.value = null
  }
}

function openDetail(skill: Skill) {
  detailOpen.value = true
  loadDetail(skill)
}

function confirmUninstall(skillName: string) {
  uninstallTargetName.value = skillName
  // Find the correct scope for this skill from its dataSource/location/agentId
  const skill = installedSkills.value.find((s) => s.slug === skillName || s.name === skillName)
  if (skill) {
    let targetScope: InstallTargetScope = selectedScope.value
    if (skill.location === 'workspace' && skill.agentId) {
      targetScope = (`agent-workspace:` + skill.agentId) as InstallTargetScope
    } else if (skill.dataSource === 'managed' || skill.dataSource === 'bundled' || skill.dataSource === 'gateway') {
      targetScope = 'openclaw-shared' as InstallTargetScope
    }
    uninstallTargetScopeOverride.value = targetScope
  } else {
    uninstallTargetScopeOverride.value = selectedScope.value
  }
}

async function openInstallTargetDir() {
  const target = installTargets.value.find((t) => t.scope === selectedScope.value)
  console.log('[SkillMarket] openInstallTargetDir: selectedScope=', selectedScope.value, 'target=', target)
  if (target?.path) {
    console.log('[SkillMarket] openInstallTargetDir: calling BrowserService.OpenDirectory with path=', target.path)
    await BrowserService.OpenDirectory(target.path)
  } else {
    console.warn('[SkillMarket] openInstallTargetDir: no target found for scope', selectedScope.value)
  }
}

async function onScopeChange(scope: InstallTargetScope) {
  selectedScope.value = scope
  if (activeTab.value === 'installed') {
    await loadInstalledSkills()
  }
}

async function loadSharedDir() {
  if (sharedDir.value) return
  try {
    sharedDir.value = await OpenClawSkillsService.GetManagedSkillsRoot()
  } catch {
    sharedDir.value = ''
  }
}

async function handleAddSkill() {
  addDialogOpen.value = true
}

async function handleCreateViaChat() {
  addDialogOpen.value = false
  const isWorkspace = selectedScope.value.startsWith('agent-workspace:')

  // 无论共享目录还是 agent 工作目录，都跳转到任务助手并发提示词
  await loadSharedDir()
  const sharedDirPath = sharedDir.value || ''
  navigationStore.navigateToModule('openclaw', appStore.currentSystem)

  window.setTimeout(async () => {
    let prompt = ''
    if (isWorkspace) {
      const agentId = selectedScope.value.replace('agent-workspace:', '')
      const target = installTargets.value.find((t) => t.scope === selectedScope.value)
      const agentName = target?.label || `#${agentId}`
      const agentDir = target?.path || ''
      prompt = t('settings.skillMarket.addSkillViaChatPrompt', {
        agentId,
        agentName,
        dir: agentDir,
      })
    } else {
      prompt = t('settings.skillMarket.addSkillViaChatPromptShared', { dir: sharedDirPath })
    }
    Events.Emit('text-selection:send-to-assistant', { text: prompt })
    Events.Emit('openclaw:expand-sidebar')
  }, 150)
}

async function handleOpenDirectory() {
  addDialogOpen.value = false
  const isWorkspace = selectedScope.value.startsWith('agent-workspace:')
  if (!isWorkspace) {
    await loadSharedDir()
    const dir = sharedDir.value || ''
    if (sharedDir.value) {
      await BrowserService.OpenDirectory(sharedDir.value)
    }
    showToast({
      title: t('settings.skillMarket.addSkillHint'),
      description: dir
        ? t('settings.skillMarket.addSkillHintDescShared', { dir })
        : t('settings.skillMarket.addSkillHintDesc'),
    })
    return
  }
  const agentId = selectedScope.value.replace('agent-workspace:', '')
  if (!agentId) return
  const target = installTargets.value.find((t) => t.scope === selectedScope.value)
  if (target?.path) {
    await BrowserService.OpenDirectory(target.path)
  }
  showToast({
    title: t('settings.skillMarket.addSkillHint'),
    description: t('settings.skillMarket.addSkillHintDescAgent', {
      dir: target?.path || '',
    }),
  })
}

function handleTabChange(tab: PageTab) {
  activeTab.value = tab
  if (tab === 'installed') {
    loadInstalledSkills()
  }
}

watch(selectedScope, () => {
  if (activeTab.value === 'installed') {
    loadInstalledSkills()
  }
})

watch(activeTab, (tab) => {
  if (tab === 'browse' && skills.value.length === 0) {
    void loadBrowseSkills(false)
  }
})

onMounted(async () => {
  await Promise.all([
    loadAgents(),
    loadInstallTargets(),
    loadCategories(),
    loadInstalledSkills(),
  ])
})
</script>

<template>
  <div class="flex h-full flex-col bg-background">
    <div class="flex shrink-0 items-center justify-between border-b px-4 py-3">
      <div class="flex flex-col">
        <span class="text-sm font-medium text-foreground">{{ t('settings.skillMarket.listHeading') }}</span>
        <span class="text-xs text-muted-foreground">{{ t('settings.skillMarket.listSubheading') }}</span>
      </div>
    </div>

    <div class="flex shrink-0 items-center gap-3 border-b bg-muted/30 px-4 py-2">
      <span class="text-xs text-muted-foreground">安装目录:</span>
      <button
        class="inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
        :class="
          selectedScope === InstallTargetScope.ScopeOpenClawShared
            ? 'bg-foreground text-background'
            : 'bg-muted text-muted-foreground hover:bg-accent hover:text-foreground'
        "
        @click="setOpenClawShared"
      >
        <Package class="size-3" />
        共享技能目录
      </button>

      <span class="ml-2 text-xs text-muted-foreground">agent:</span>
      <div v-if="agentsLoading" class="flex items-center gap-1 text-xs text-muted-foreground">
        <Loader2 class="size-3 animate-spin" />
        {{ t('settings.skillMarket.loadingAgents') }}
      </div>
      <div v-else class="flex items-center gap-1.5">
        <select
          v-model="selectedAgentId"
          class="rounded-md border bg-background px-2 py-1 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-ring"
        >
          <option :value="null">{{ t('settings.skillMarket.agentNone') }}</option>
          <option v-for="agent in agents" :key="agent.id" :value="agent.id">
            {{ agent.name }}
          </option>
        </select>
      </div>

      <TooltipProvider :delay-duration="300">
        <Tooltip>
          <TooltipTrigger as-child>
            <button
              class="inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
              :class="
                selectedScope.startsWith('agent-workspace:')
                  ? 'bg-foreground text-background'
                  : 'bg-muted text-muted-foreground hover:bg-accent hover:text-foreground'
              "
              @click="setAgentWorkspace"
              :disabled="!selectedAgentId"
            >
              <Package class="size-3" />
              agent工作目录
            </button>
          </TooltipTrigger>
          <TooltipContent side="bottom" :side-offset="4">
            <p class="max-w-xs break-all text-xs">
              <template v-if="agentWorkspaceTarget?.path">
                {{ agentWorkspaceTarget.path }}
              </template>
              <template v-else-if="selectedAgentId">
                {{ t('settings.skillMarket.agentWorkspaceDirLoading') }}
              </template>
              <template v-else>
                {{ t('settings.skillMarket.agentWorkspaceDirHint') }}
              </template>
            </p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>

      <div class="ml-auto flex items-center gap-2">
        <button
          class="inline-flex items-center gap-1.5 rounded-md bg-muted px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          @click="handleRefresh"
        >
          <RefreshCw class="size-3" />
          {{ t('settings.skillMarket.refreshCta') }}
        </button>
        <button
          class="inline-flex items-center gap-1.5 rounded-md bg-foreground px-3 py-1.5 text-xs font-medium text-background transition-opacity hover:opacity-80"
          @click="handleAddSkill"
        >
          <Plus class="size-3" />
          添加技能
        </button>
        <button
          class="inline-flex items-center gap-1.5 rounded-md bg-foreground px-3 py-1.5 text-xs font-medium text-background transition-opacity hover:opacity-80"
          @click="openInstallTargetDir"
        >
          <FolderOpen class="size-3" />
          {{ t('settings.skillMarket.openDir') }}
        </button>
      </div>
    </div>

    <div class="flex shrink-0 items-center gap-1 border-b bg-muted/20 px-4 py-2">
      <button
        class="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
        :class="
          activeTab === 'installed'
            ? 'bg-background text-foreground shadow-sm'
            : 'text-muted-foreground hover:bg-muted hover:text-foreground'
        "
        @click="handleTabChange('installed')"
      >
        {{ t('settings.skillMarket.tabInstalled') }}
        <Badge variant="secondary" class="ml-0.5 px-1.5 py-0 text-[10px]">
          {{ scopeFilteredInstalledSkills.length }}
        </Badge>
      </button>
      <button
        class="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
        :class="
          activeTab === 'browse'
            ? 'bg-background text-foreground shadow-sm'
            : 'text-muted-foreground hover:bg-muted hover:text-foreground'
        "
        @click="handleTabChange('browse')"
      >
        {{ t('settings.skillMarket.tabBrowse') }}
        <Badge variant="secondary" class="ml-0.5 px-1.5 py-0 text-[10px]">
          {{ totalCount }}
        </Badge>
      </button>

      <div class="ml-auto flex items-center gap-2">
        <div class="relative">
          <Search class="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            v-if="activeTab === 'browse'"
            v-model="searchQuery"
            :placeholder="t('settings.skillMarket.searchPlaceholder')"
            class="h-8 w-52 pl-8 text-xs"
            @keydown.enter="handleSearch"
          />
          <Input
            v-else
            v-model="installedSearchQuery"
            placeholder="搜索技能..."
            class="h-8 w-52 pl-8 text-xs"
            @keydown.enter="void 0"
          />
        </div>
      </div>
    </div>

    <div v-if="activeTab === 'browse'" class="flex flex-1 flex-col overflow-hidden">
      <div class="flex shrink-0 gap-2 overflow-x-auto border-b bg-muted/20 px-4 py-2">
        <button
          class="inline-flex shrink-0 items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
          :class="
            selectedCategoryId === null
              ? 'bg-foreground text-background'
              : 'bg-muted text-muted-foreground hover:bg-accent hover:text-foreground'
          "
          @click="handleCategoryClick(null)"
        >
          {{ t('settings.skillMarket.filterAll') }}
          <span class="text-[10px] opacity-60">{{ totalSkillCount }}</span>
        </button>
        <button
          v-for="cat in categories"
          :key="cat.id"
          class="inline-flex shrink-0 items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
          :class="
            selectedCategoryId === cat.id
              ? 'bg-foreground text-background'
              : 'bg-muted text-muted-foreground hover:bg-accent hover:text-foreground'
          "
          @click="handleCategoryClick(cat.id)"
        >
          {{ cat.nameLocal || cat.name }}
          <span class="text-[10px] opacity-60">{{ cat.skillCount }}</span>
        </button>
      </div>

      <div ref="browseListEl" class="flex-1 overflow-y-auto p-4" @scroll="handleBrowseScroll">
        <TooltipProvider :delay-duration="300">
          <div v-if="browseLoading && skills.length === 0" class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          <div
            v-for="i in 8"
            :key="i"
            class="animate-pulse rounded-xl border bg-card p-4"
          >
            <div class="mb-3 h-12 w-12 rounded-lg bg-muted" />
            <div class="mb-2 h-4 w-3/4 rounded bg-muted" />
            <div class="h-3 w-full rounded bg-muted" />
          </div>
        </div>

        <div
          v-else-if="!browseLoading && skills.length === 0"
          class="flex h-48 flex-col items-center justify-center gap-2"
        >
          <Grid2X2 class="size-8 text-muted-foreground/50" />
          <p class="text-sm text-muted-foreground">{{ t('settings.skillMarket.noSkills') }}</p>
          <p class="text-xs text-muted-foreground/70">{{ t('settings.skillMarket.noSkillsHint') }}</p>
        </div>

        <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          <div
            v-for="skill in skills"
            :key="skill.id"
            class="group flex cursor-pointer flex-col rounded-2xl border border-[#d9d9d9] bg-white p-4 shadow-sm transition-shadow hover:shadow-md dark:bg-card"
            @click="openDetail(skill)"
          >
            <!-- 顶行：icon + 名称 + skill_name + 分类 -->
            <div class="mb-3 flex items-start gap-3">
              <div
                v-if="skill.iconUrl"
                class="size-14 shrink-0 overflow-hidden rounded-md"
              >
                <img :src="skill.iconUrl" :alt="skill.name" class="h-full w-full object-cover" />
              </div>
              <div
                v-else
                class="flex size-14 shrink-0 items-center justify-center rounded-md"
                style="background: #fef2f2"
              >
                <Package class="size-6" style="color: #ef4444" />
              </div>
              <div class="min-w-0 flex-1">
                <p class="truncate text-sm font-medium leading-snug text-[#171717] dark:text-foreground">{{ skill.name || skill.skillName }}</p>
                <p class="mt-0.5 truncate text-xs leading-4 text-[#737373]">{{ skill.skillName }}</p>
                <span
                  v-if="skill.categoryName"
                  class="mt-1 inline-block rounded bg-neutral-100 px-1.5 py-0.5 text-xs text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400"
                >{{ skill.categoryName }}</span>
              </div>
            </div>

            <!-- 介绍 -->
            <p class="mb-3 line-clamp-2 min-h-8 text-xs leading-4 text-[#737373]">{{ skill.description }}</p>

            <!-- 底行：来源 + 安装状态 -->
            <div class="flex items-center justify-between">
              <span class="text-xs text-[#737373]">{{ sourceLabel(skill.source) }}</span>
                <Tooltip>
                  <TooltipTrigger as-child>
                    <button
                      v-if="skill.isBuiltin"
                      class="inline-flex size-6 items-center justify-center rounded-md"
                      disabled
                    >
                      <Check class="size-4 text-emerald-500" />
                    </button>
                    <button
                      v-else-if="installingSet.has(skill.skillName)"
                      class="inline-flex size-6 items-center justify-center rounded-md"
                      disabled
                    >
                      <Loader2 class="size-4 animate-spin text-amber-500" />
                    </button>
                    <button
                      v-else
                      class="inline-flex size-6 items-center justify-center rounded-md"
                      @click.stop="openInstallDialog(skill)"
                    >
                      <Download class="size-4 text-[#737373]" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom" :side-offset="4">
                    <span v-if="skill.isBuiltin">已安装</span>
                    <span v-else-if="installingSet.has(skill.skillName)">安装中</span>
                    <span v-else>安装技能</span>
                  </TooltipContent>
                </Tooltip>
            </div>
          </div>
        </div>

        <div v-if="browseLoading && skills.length > 0" class="flex items-center justify-center py-6">
          <Loader2 class="size-5 animate-spin text-muted-foreground" />
        </div>
        </TooltipProvider>
      </div>
    </div>

    <div v-else class="flex min-h-0 flex-1 flex-col">
      <!-- Skill list -->
      <div class="flex-1 overflow-auto px-4 pb-4">
        <div
          v-if="installedLoading"
          class="flex items-center justify-center py-12"
        >
          <Loader2 class="size-5 animate-spin text-muted-foreground" />
        </div>

        <div
          v-else-if="scopeFilteredInstalledSkills.length === 0"
          class="flex flex-col items-center justify-center gap-2 py-12 text-muted-foreground"
        >
          <Package class="size-8 opacity-40" />
          <span class="text-sm">{{ t('settings.skillMarket.noSkills') }}</span>
        </div>

        <div v-else>
          <div class="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-3">
            <div
              v-for="skill in scopeFilteredInstalledSkills"
              :key="skill.slug"
              class="group relative flex cursor-pointer flex-col rounded-2xl border border-[#d9d9d9] bg-white p-4 shadow-sm transition-shadow hover:shadow-md dark:border-white/10 dark:bg-card"
              @click="showInstalledDetail(skill)"
            >
              <!-- Top row: icon + name + tag + description -->
              <div class="mb-3 flex items-start gap-3">
                <!-- Icon -->
                <div
                  class="flex size-14 shrink-0 items-center justify-center rounded-md"
                  style="background: #fef2f2"
                >
                  <span v-if="skill.icon" class="text-xl">{{ skill.icon }}</span>
                  <SquareDashedMousePointer v-else class="size-6" style="color: #ef4444" />
                </div>
                <!-- Name + tag + description -->
                <div class="min-w-0 flex-1">
                  <div class="mb-1 flex items-center gap-2">
                    <span class="truncate text-sm font-medium leading-snug text-[#171717] dark:text-foreground">
                      {{ skill.name || skill.slug }}
                    </span>
                    <span
                      class="shrink-0 rounded-full bg-neutral-100 px-2 py-0.5 text-xs text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400"
                    >
                      {{ skill.dataSource === 'extra' ? '内置' : (skill.location === 'workspace' ? '工作区' : skill.dataSource || skill.location) }}
                    </span>
                  </div>
                  <p
                    v-if="skill.description"
                    class="line-clamp-2 min-h-[2lh] text-xs leading-4 text-neutral-500"
                  >
                    {{ skill.description }}
                  </p>
                  <div v-else class="min-h-[2lh]" />
                </div>
              </div>

              <!-- Divider -->
              <div class="mb-3 h-px w-full rounded-sm bg-neutral-200" />

              <!-- Bottom row: author/agent + toggle + settings -->
              <div class="flex items-center justify-between">
                <span class="text-xs text-neutral-500">
                  {{ skill.agentName || (skill.location === 'workspace' ? `Agent #${skill.agentId}` : 'OpenClaw') }}
                </span>
                <div class="flex items-center gap-2">
                  <!-- Switch toggle -->
                  <div class="flex items-center" :title="skill.eligible === false ? t('settings.skillMarket.enable') : t('settings.skillMarket.disable')">
                    <Switch
                      size="sm"
                      :model-value="skill.eligible === true"
                      :disabled="skill.eligible == null || skillTogglePending.has(skill.slug)"
                      @click.stop
                      @update:model-value="toggleSkillEnabled(skill.slug, skill.eligible)"
                    />
                  </div>

                  <!-- Uninstall button -->
                  <button
                    class="flex size-6 items-center justify-center rounded-md text-neutral-400 transition-colors hover:bg-red-50 hover:text-red-500 dark:hover:bg-red-500/10"
                    :title="t('settings.skills.uninstall')"
                    @click.stop="confirmUninstall(skill.slug)"
                  >
                    <Trash2 class="size-4" />
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- ==================== DETAIL OVERLAY (browse tab) ==================== -->
    <Dialog :open="detailOpen && !installedDetailOpen" @update:open="(v) => !v && (detailOpen = false)">
      <DialogContent class="flex max-h-[80vh] max-w-xl flex-col overflow-hidden p-0">
        <div class="relative flex flex-col gap-2 px-4 pt-4">
          <button
            class="absolute right-4 top-4 text-muted-foreground hover:text-foreground"
            @click="detailOpen = false"
          >
            <svg class="size-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>

          <div class="flex flex-col items-center gap-2">
            <div
              v-if="detailSkill?.iconUrl"
              class="size-[62px] overflow-hidden rounded-lg"
            >
              <img :src="detailSkill.iconUrl" :alt="detailTitle" class="h-full w-full object-cover" />
            </div>
            <div
              v-else
              class="flex size-[62px] items-center justify-center rounded-lg"
              style="background: #fef2f2"
            >
              <Package class="size-8" style="color: #ef4444" />
            </div>
            <span class="text-base font-semibold text-[#171717] dark:text-foreground">{{ detailTitle }}</span>
            <span class="text-sm text-[#404040]">{{ detailSkill?.skillName }}</span>
            <Badge
              v-if="detailSkill?.categoryName"
              variant="outline"
              class="mt-0.5"
            >
              {{ detailSkill.categoryName }}
            </Badge>
          </div>
        </div>

        <div class="flex flex-1 flex-col overflow-y-auto px-6 pb-4">
          <div class="mb-3 h-px bg-neutral-200" />

          <div class="mb-2 flex items-center gap-2">
            <svg class="size-4 shrink-0" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span class="text-sm font-semibold text-[#171717]">技能介绍</span>
          </div>
          <p class="mb-3 text-sm leading-5 text-[#404040]">{{ detailSkill?.description }}</p>

          <div class="mb-3 h-px bg-neutral-300" />

          <div class="mb-3 flex items-center gap-2">
            <svg class="size-4 shrink-0" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
            </svg>
            <span class="text-sm font-semibold text-[#171717]">怎么使用？</span>
          </div>
          <div
            v-if="detailSkill?.instructions"
            class="mb-4 rounded-xl bg-neutral-100 p-4 text-sm dark:bg-neutral-800"
          >
            <MarkdownRenderer :content="detailSkill.instructions" />
          </div>

          <div class="mt-auto flex flex-col items-center gap-2">
            <div class="flex w-full items-center gap-2">
              <button
                v-if="detailIsInstalled"
                class="flex flex-1 items-center justify-center gap-2 rounded-lg bg-[#171717] px-4 py-2 text-sm font-medium text-[#fafafa] opacity-50"
                disabled
              >
                {{ t('settings.skills.added') }}
              </button>
              <button
                v-else-if="detailSkill"
                class="flex flex-1 items-center justify-center gap-2 rounded-lg bg-[#171717] px-4 py-2 text-sm font-medium text-[#fafafa] transition-opacity hover:opacity-80"
                :disabled="installingSet.has(detailSkill.skillName)"
                @click="openInstallDialog(detailSkill)"
              >
                <Loader2 v-if="installingSet.has(detailSkill.skillName)" class="size-4 animate-spin" />
                <Plus v-else class="size-4" />
                {{ installingSet.has(detailSkill.skillName) ? t('settings.skills.installing') : t('settings.skills.install') }}
              </button>
              <button
                v-if="detailIsInstalled && !installedDetailIsBuiltIn"
                class="flex size-9 items-center justify-center rounded-lg border border-[#e5e5e5] bg-white shadow-sm hover:bg-neutral-100 dark:bg-neutral-900 dark:border-neutral-700"
                :title="t('settings.skillMarket.builtInCannotUninstall')"
                @click="detailSkill && confirmUninstall(detailSkill.skillName)"
              >
                <Trash2 class="size-4 text-neutral-600 dark:text-neutral-400" />
              </button>
            </div>

            <div class="flex items-center gap-2 text-sm text-[#737373]">
              <Shield class="size-4 shrink-0" />
              <span>已通过安全与合规验证，无恶意代码或数据泄露风险。</span>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>

    <!-- ==================== DETAIL OVERLAY (installed tab - SkillsPage style) ==================== -->
    <div
      v-if="installedDetailOpen"
      class="absolute inset-0 z-50 flex flex-col overflow-hidden bg-background"
    >
      <!-- Row 1: back button -->
      <div class="flex shrink-0 items-center border-b border-border px-4 py-2">
        <button
          class="inline-flex cursor-pointer items-center gap-1 rounded-md px-1 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          @click="installedDetailOpen = false"
        >
          <ChevronLeft class="size-4" />
          {{ t('settings.skillMarket.tabInstalled') }}
        </button>
      </div>

      <!-- Row 2: skill info + actions -->
      <div class="flex shrink-0 items-start justify-between gap-4 border-b border-border px-4 py-3">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <span class="text-base font-semibold text-foreground">{{ installedDetailName }}</span>
            <Badge
              v-if="installedDetailSkill"
              variant="secondary"
              class="shrink-0 bg-muted px-1.5 py-0 text-[10px] text-muted-foreground"
            >
              {{ installedDetailSkill.dataSource === 'extra' ? '内置技能' : (installedDetailSkill.location === 'workspace' ? '工作区' : installedDetailSkill.dataSource || installedDetailSkill.location) }}
            </Badge>
          </div>
          <p v-if="installedDetailDescription" class="mt-1 text-xs leading-relaxed text-muted-foreground">
            {{ installedDetailDescription }}
          </p>
        </div>

        <div class="flex shrink-0 flex-col items-end gap-2">
          <span v-if="installedDetailVersion" class="text-xs text-muted-foreground">v{{ installedDetailVersion }}</span>

          <div class="flex items-center gap-2">
            <button
              class="inline-flex cursor-pointer items-center gap-1.5 rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              @click="openInstalledSkillDir"
            >
              <FolderOpen class="size-3.5" />
              {{ t('settings.skills.openDir') }}
            </button>
          </div>
        </div>
      </div>

      <!-- Detail body: file list + content -->
      <div v-if="installedDetailLoading" class="flex flex-1 items-center justify-center">
        <Loader2 class="size-5 animate-spin text-muted-foreground" />
      </div>
      <div v-else class="flex min-h-0 flex-1">
        <!-- Left: file list -->
        <aside class="flex w-56 shrink-0 flex-col overflow-auto border-r border-border bg-muted/30">
          <div
            v-for="file in installedDetailFiles"
            :key="file.path"
            :title="file.path + '  ' + formatFileSize(file.size)"
            :class="
              cn(
                'flex cursor-pointer items-center gap-2 px-3 py-1.5 text-xs transition-colors hover:bg-accent/50',
                selectedFilePath === file.path && 'bg-accent text-foreground',
                selectedFilePath !== file.path && 'text-muted-foreground'
              )
            "
            @click="selectFile(file.path)"
          >
            <FileText v-if="isMarkdownFile(file.path)" class="size-3.5 shrink-0" />
            <FileCode v-else class="size-3.5 shrink-0" />
            <span class="min-w-0 flex-1 truncate">{{ file.path }}</span>
            <span class="shrink-0 text-[10px] opacity-60">{{ formatFileSize(file.size) }}</span>
          </div>
          <div v-if="installedDetailFiles.length === 0" class="px-3 py-4 text-xs text-muted-foreground">
            {{ t('settings.skills.noDetailContent') }}
          </div>
        </aside>

        <!-- Right: file content -->
        <main class="skill-file-viewer flex min-w-0 flex-1 flex-col overflow-auto">
          <div v-if="fileLoading" class="flex flex-1 items-center justify-center">
            <Loader2 class="size-5 animate-spin text-muted-foreground" />
          </div>
          <div
            v-else-if="!selectedFilePath"
            class="flex flex-1 items-center justify-center text-sm text-muted-foreground"
          >
            {{ t('settings.skills.selectFile') }}
          </div>
          <div
            v-else-if="isBinaryFile(selectedFilePath)"
            class="flex flex-1 items-center justify-center text-sm text-muted-foreground"
          >
            {{ t('settings.skills.binaryFile') }}
          </div>
          <div v-else-if="isSelectedFileMarkdown" class="p-4">
            <MarkdownRenderer :content="renderedFileContent || ''" />
          </div>
          <div v-else>
            <MarkdownRenderer :content="fileContentAsMarkdown" />
          </div>
        </main>
      </div>
    </div>

    <AlertDialog :open="!!uninstallTargetName" @update:open="(v) => !v && (uninstallTargetName = null) && (uninstallTargetScopeOverride = null)">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{{ isBuiltInUninstall ? t('settings.skillMarket.builtInCannotUninstall') : t('settings.skills.uninstall') }}</AlertDialogTitle>
          <AlertDialogDescription>
            <template v-if="isBuiltInUninstall">
              {{ t('settings.skillMarket.builtInUninstallHint') }}
            </template>
            <template v-else>
              {{ t('settings.skillMarket.deleteConfirm') }} "{{ uninstallTargetName }}"
            </template>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{{ t('common.cancel') }}</AlertDialogCancel>
          <AlertDialogAction
            v-if="!isBuiltInUninstall"
            class="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            @click="uninstallTargetName && handleUninstall(uninstallTargetName)"
          >
            {{ t('settings.skills.uninstall') }}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <Dialog :open="installDialogOpen" @update:open="(v) => !v && (installDialogOpen = false)">
      <DialogContent class="max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t('settings.skillMarket.installTitle') }}</DialogTitle>
        </DialogHeader>
        <div class="py-4">
          <p class="mb-3 text-sm text-muted-foreground">
            {{ t('settings.skillMarket.installDescription') }}
          </p>
          <div class="space-y-2">
            <label class="text-xs font-medium text-foreground">{{ t('settings.skillMarket.selectTarget') }}</label>
            <div v-if="targetsLoading" class="flex items-center gap-2 text-xs text-muted-foreground">
              <Loader2 class="size-3 animate-spin" />
              {{ t('settings.skillMarket.loadingTargets') }}
            </div>
            <div v-else class="flex flex-col gap-1.5">
              <button
                v-for="tgt in installTargets"
                :key="tgt.scope"
                class="flex items-center gap-2 rounded-md border px-3 py-2 text-left text-xs transition-colors"
                :class="
                  tgt.scope === selectedScope
                    ? 'border-foreground bg-muted text-foreground'
                    : 'border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                "
                :disabled="!tgt.available"
                @click="tgt.available && (selectedScope = tgt.scope)"
              >
                <Package class="size-3.5 shrink-0" />
                <span class="flex-1">{{ scopeLabel(tgt.scope) }}</span>
                <span v-if="tgt.openClawAgentId" class="text-[10px] text-muted-foreground/70">#{{ tgt.openClawAgentId }}</span>
              </button>
            </div>
          </div>
        </div>
        <DialogFooter>
          <button
            class="inline-flex items-center gap-1.5 rounded-md bg-muted px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            @click="installDialogOpen = false"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            class="inline-flex items-center gap-1.5 rounded-md bg-foreground px-3 py-1.5 text-xs font-medium text-background transition-opacity hover:opacity-80"
            :disabled="installDialogLoading"
            @click="handleInstall"
          >
            <Loader2 v-if="installDialogLoading" class="size-3 animate-spin" />
            {{ t('settings.skills.install') }}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog :open="addDialogOpen" @update:open="(v) => (addDialogOpen = v)">
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle>{{ t('settings.skillMarket.addSkillDialogTitle') }}</DialogTitle>
        </DialogHeader>

        <div class="grid gap-3 py-2">
          <button
            type="button"
            class="flex w-full items-start gap-3 rounded-xl border border-border bg-background p-4 text-left transition-colors hover:bg-accent/30"
            @click="handleCreateViaChat"
          >
            <div
              class="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground"
            >
              <Plus class="size-4" />
            </div>
            <div class="min-w-0">
              <div class="text-sm font-medium text-foreground">
                {{ t('settings.skillMarket.addSkillViaChatTitle') }}
              </div>
              <div class="mt-1 text-xs leading-relaxed text-muted-foreground">
                {{ t('settings.skillMarket.addSkillViaChatDesc') }}
              </div>
            </div>
          </button>

          <button
            type="button"
            class="flex w-full items-start gap-3 rounded-xl border border-border bg-background p-4 text-left transition-colors hover:bg-accent/30"
            @click="handleOpenDirectory"
          >
            <div
              class="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground"
            >
              <FolderOpen class="size-4" />
            </div>
            <div class="min-w-0">
              <div class="text-sm font-medium text-foreground">
                {{ t('settings.skillMarket.addSkillChoosePackageTitle') }}
              </div>
              <div class="mt-1 text-xs leading-relaxed text-muted-foreground">
                {{ t('settings.skillMarket.addSkillChoosePackageDesc') }}
              </div>
            </div>
          </button>
        </div>
      </DialogContent>
    </Dialog>
  </div>
</template>
