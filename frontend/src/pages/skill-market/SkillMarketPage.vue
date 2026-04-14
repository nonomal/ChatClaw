<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  RefreshCw,
  Search,
  Loader2,
  FolderOpen,
  Plus,
  Check,
  Download,
  Trash2,
  Grid2X2,
  Package,
} from 'lucide-vue-next'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import MarkdownRenderer from '@/components/MarkdownRenderer.vue'
import { toast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'
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
  SkillDetail,
  InstallTargetConfig,
} from '@bindings/chatclaw/internal/services/skillmarket'
import { BrowserService } from '@bindings/chatclaw/internal/services/browser'
import { OpenClawAgentsService, type OpenClawAgent } from '@bindings/chatclaw/internal/openclaw/agents'

const props = defineProps<{
  tabId: string
}>()

const { t, locale } = useI18n()

type PageTab = 'installed' | 'browse'

const activeTab = ref<PageTab>('browse')

const agents = ref<OpenClawAgent[]>([])
const selectedAgentId = ref<number | null>(null)
const agentsLoading = ref(false)

const installTargets = ref<InstallTargetConfig[]>([])
const selectedScope = ref<InstallTargetScope>(InstallTargetScope.ScopeLocal)
const targetsLoading = ref(false)

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
    if (scopes.length > 0 && scopes[0].available) {
      selectedScope.value = scopes[0].scope
    }
  } catch (error) {
    console.error('Failed to load install targets:', error)
  } finally {
    targetsLoading.value = false
  }
}

// Reload targets when agent changes
watch(selectedAgentId, () => {
  loadInstallTargets()
})

const categories = ref<SkillCategory[]>([])
const skills = ref<Skill[]>([])
const totalCount = ref(0)
const totalSkillCount = computed(() => categories.value.reduce((sum, cat) => sum + (cat.skillCount || 0), 0))
const browseLoading = ref(false)
const browsePage = ref(1)
const BROWSE_PAGE_SIZE = 24
const selectedCategoryId = ref<number | null>(null)
const searchQuery = ref('')
const browseHasMore = computed(() => skills.value.length < totalCount.value)

const installedNames = ref<string[]>([])
const installedLoading = ref(false)
const installedPage = ref(1)
const INSTALLED_PAGE_SIZE = 20

const paginatedInstalledNames = computed(() =>
  installedNames.value.slice(0, installedPage.value * INSTALLED_PAGE_SIZE)
)

const installedHasMore = computed(
  () => paginatedInstalledNames.value.length < installedNames.value.length
)

const detailOpen = ref(false)
const detailSkill = ref<Skill | null>(null)
const detailMeta = ref<SkillDetail | null>(null)
const detailLoading = ref(false)
const selectedFilePath = ref('')
const fileContent = ref('')
const fileLoading = ref(false)
let detailLoadVersion = 0
let fileLoadVersion = 0

const detailTitle = computed(() => {
  if (!detailMeta.value) return ''
  return detailMeta.value.name || detailMeta.value.skillName || ''
})

const detailDescription = computed(() => {
  if (!detailMeta.value) return ''
  return detailMeta.value.description || ''
})

const detailIsInstalled = computed(() => {
  if (detailMeta.value?.isBuiltin) return true
  return detailSkill.value ? installedNames.value.includes(detailSkill.value.skillName) : false
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
    })
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

async function loadInstalledSkills() {
  installedLoading.value = true
  try {
    installedNames.value = await SkillMarketService.GetInstalledSkillNames(selectedScope.value)
    installedPage.value = 1
  } catch (error) {
    console.error('Failed to load installed skills:', error)
  } finally {
    installedLoading.value = false
  }
}

async function loadDetail(skill: Skill) {
  const version = ++detailLoadVersion
  detailSkill.value = skill
  detailMeta.value = null
  selectedFilePath.value = ''
  fileContent.value = ''
  detailLoading.value = true
  try {
    const meta = await SkillMarketService.GetSkillDetail(skill.id, locale.value)
    if (version !== detailLoadVersion) return
    detailMeta.value = meta
  } catch {
    if (version !== detailLoadVersion) return
  } finally {
    if (version === detailLoadVersion) {
      detailLoading.value = false
    }
  }
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
    // Remote file preview not yet implemented
    fileContent.value = `// ${t('settings.skillMarket.filePreviewNA')} // ${path}`
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
  } else {
    installedPage.value = 1
  }
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
    await loadInstalledSkills()
    const idx = skills.value.findIndex((s) => s.id === skill.id)
    if (idx !== -1) skills.value[idx].isBuiltin = true
  } catch (error) {
    toast.error(getErrorMessage(error) || t('settings.skillMarket.installFailed'))
  } finally {
    installDialogLoading.value = false
  }
}

const uninstallTargetName = ref<string | null>(null)

async function handleUninstall(skillName: string) {
  try {
    await SkillMarketService.UninstallSkill(skillName, selectedScope.value)
    toast.success(t('settings.skillMarket.uninstallSuccess'))
    installedNames.value = installedNames.value.filter((n) => n !== skillName)
    const idx = skills.value.findIndex((s) => s.skillName === skillName)
    if (idx !== -1) skills.value[idx].isBuiltin = false
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
}

async function openInstallTargetDir() {
  const target = installTargets.value.find((t) => t.scope === selectedScope.value)
  if (target?.path) {
    await BrowserService.OpenDirectory(target.path)
  }
}

async function onScopeChange(scope: InstallTargetScope) {
  selectedScope.value = scope
  if (activeTab.value === 'installed') {
    await loadInstalledSkills()
  }
}

function handleTabChange(tab: PageTab) {
  activeTab.value = tab
  if (tab === 'installed') {
    loadInstalledSkills()
  }
}

onMounted(async () => {
  await Promise.all([
    loadAgents(),
    loadInstallTargets(),
    loadCategories(),
    loadBrowseSkills(false),
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
      <div class="flex items-center gap-2">
        <button
          class="inline-flex items-center gap-1.5 rounded-md bg-muted px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          @click="handleRefresh"
        >
          <RefreshCw class="size-3" />
          {{ t('settings.skillMarket.refreshCta') }}
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

    <div class="flex shrink-0 items-center gap-3 border-b bg-muted/30 px-4 py-2">
      <span class="text-xs text-muted-foreground">{{ t('settings.skillMarket.selectAgent') }}:</span>
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

      <span class="ml-2 text-xs text-muted-foreground">{{ t('settings.skillMarket.selectTarget') }}:</span>
      <div v-if="targetsLoading" class="flex items-center gap-1 text-xs text-muted-foreground">
        <Loader2 class="size-3 animate-spin" />
      </div>
      <div v-else class="flex items-center gap-1.5">
        <button
          v-for="tgt in installTargets"
          :key="tgt.scope"
          class="inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
          :class="
            tgt.scope === selectedScope
              ? 'bg-foreground text-background'
              : tgt.available
                ? 'bg-muted text-muted-foreground hover:bg-accent hover:text-foreground'
                : 'cursor-not-allowed bg-muted/50 text-muted-foreground/50'
          "
          :disabled="!tgt.available"
          @click="tgt.available && onScopeChange(tgt.scope)"
        >
          <Package class="size-3" />
          {{ scopeLabel(tgt.scope) }}
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
          {{ installedNames.length }}
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
            v-model="searchQuery"
            :placeholder="t('settings.skillMarket.searchPlaceholder')"
            class="h-8 w-52 pl-8 text-xs"
            @keydown.enter="handleSearch"
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

      <div class="flex-1 overflow-y-auto p-4">
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
            <!-- 顶行：icon + 名称 + 描述 -->
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
                <p class="mt-0.5 truncate text-xs leading-4 text-[#737373]">{{ skill.description }}</p>
              </div>
            </div>

            <!-- 分隔线 -->
            <div class="mb-3 h-px bg-neutral-200" />

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
        </TooltipProvider>

        <div v-if="browseHasMore" class="flex justify-center py-4">
          <button
            class="inline-flex cursor-pointer items-center gap-1 rounded-md bg-muted px-4 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            :disabled="browseLoading"
            @click="loadBrowseSkills(true)"
          >
            <Loader2 v-if="browseLoading" class="size-3 animate-spin" />
            {{ t('settings.skillMarket.loadMore') }}
          </button>
        </div>
      </div>
    </div>

    <div v-else class="flex flex-1 flex-col overflow-hidden">
      <div class="flex-1 overflow-y-auto p-4">
        <div v-if="installedLoading && installedNames.length === 0" class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          <div
            v-for="i in 8"
            :key="i"
            class="animate-pulse rounded-xl border bg-card p-4"
          >
            <div class="h-4 w-3/4 rounded bg-muted" />
          </div>
        </div>

        <div
          v-else-if="!installedLoading && installedNames.length === 0"
          class="flex h-48 flex-col items-center justify-center gap-2"
        >
          <Package class="size-8 text-muted-foreground/50" />
          <p class="text-sm text-muted-foreground">{{ t('settings.skillMarket.noSkills') }}</p>
        </div>

        <div v-else class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          <div
            v-for="name in paginatedInstalledNames"
            :key="name"
            class="flex items-center justify-between rounded-xl border bg-card p-3 shadow-sm"
          >
            <div class="flex min-w-0 items-center gap-2">
              <div
                class="flex size-8 shrink-0 items-center justify-center rounded-lg"
                style="background: #fef2f2"
              >
                <Package class="size-4" style="color: #ef4444" />
              </div>
              <span class="truncate text-sm text-foreground">{{ name }}</span>
            </div>
            <button
              class="ml-2 shrink-0 rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
              @click="confirmUninstall(name)"
            >
              <Trash2 class="size-3.5" />
            </button>
          </div>
        </div>

        <div v-if="installedHasMore" class="flex justify-center py-4">
          <button
            class="inline-flex cursor-pointer items-center gap-1 rounded-md bg-muted px-4 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            @click="installedPage++"
          >
            {{ t('settings.skillMarket.loadMore') }}
          </button>
        </div>
      </div>
    </div>

    <Dialog :open="detailOpen" @update:open="(v) => !v && (detailOpen = false)">
      <DialogContent class="max-h-[80vh] max-w-2xl overflow-hidden p-0">
        <DialogHeader class="p-4 pb-0">
          <DialogTitle class="flex items-center gap-2">
            <div
              v-if="detailSkill?.iconUrl"
              class="size-8 overflow-hidden rounded-lg"
            >
              <img :src="detailSkill.iconUrl" :alt="detailTitle" class="h-full w-full object-cover" />
            </div>
            <div
              v-else
              class="flex size-8 items-center justify-center rounded-lg"
              style="background: #fef2f2"
            >
              <Package class="size-5" style="color: #ef4444" />
            </div>
            {{ detailTitle }}
          </DialogTitle>
        </DialogHeader>

        <div class="flex max-h-[calc(80vh-80px)] flex-col overflow-y-auto p-4 pt-2">
          <div class="mb-3 flex flex-wrap items-center gap-2">
            <Badge v-if="detailSkill?.source" variant="outline">
              {{ sourceLabel(detailSkill.source) }}
            </Badge>
            <Badge
              v-if="detailIsInstalled"
              class="bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400"
            >
              {{ t('settings.skills.installed') }}
            </Badge>
          </div>

          <p v-if="detailDescription" class="mb-4 text-sm leading-relaxed text-muted-foreground">
            {{ detailDescription }}
          </p>

          <div
            v-if="detailMeta?.instructions"
            class="mb-4 rounded-xl bg-muted/50 p-3 text-sm text-foreground"
          >
            <MarkdownRenderer :content="detailMeta.instructions" />
          </div>

          <div class="mt-4 flex items-center justify-between border-t pt-3">
            <div />
            <div class="flex items-center gap-2">
              <button
                v-if="detailIsInstalled"
                class="inline-flex items-center gap-1.5 rounded-md bg-destructive/10 px-3 py-1.5 text-xs font-medium text-destructive transition-colors hover:bg-destructive/20"
                @click="detailSkill && confirmUninstall(detailSkill.skillName)"
              >
                <Trash2 class="size-3" />
                {{ t('settings.skills.uninstall') }}
              </button>
              <button
                v-else-if="detailSkill"
                class="inline-flex items-center gap-1.5 rounded-md bg-foreground px-4 py-1.5 text-xs font-medium text-background transition-opacity hover:opacity-80"
                :disabled="installingSet.has(detailSkill.skillName)"
                @click="openInstallDialog(detailSkill)"
              >
                <Loader2 v-if="installingSet.has(detailSkill.skillName)" class="size-3 animate-spin" />
                <Plus v-else class="size-3" />
                {{ t('settings.skills.install') }}
              </button>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>

    <AlertDialog :open="!!uninstallTargetName" @update:open="(v) => !v && (uninstallTargetName = null)">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{{ t('settings.skills.uninstall') }}</AlertDialogTitle>
          <AlertDialogDescription>
            {{ t('settings.skillMarket.deleteConfirm') }} "{{ uninstallTargetName }}"
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{{ t('common.cancel') }}</AlertDialogCancel>
          <AlertDialogAction
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
  </div>
</template>
