<script setup lang="ts">
import { ref, watch, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ShieldCheck, Monitor, FolderOpen, X, Terminal, Globe, Plus, Loader2 } from 'lucide-vue-next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'
import { AgentsService, type FileEntry, UpdateAgentInput } from '@bindings/chatclaw/internal/services/agents'
import { MCPService } from '@bindings/chatclaw/internal/services/mcp'
import type { MCPServer } from '@bindings/chatclaw/internal/services/mcp'
import { BrowserService } from '@bindings/chatclaw/internal/services/browser'
import type { Agent } from '@bindings/chatclaw/internal/services/agents'
import { Events } from '@wailsio/runtime'
import FileTreeNode from './FileTreeNode.vue'

const FS_MUTATING_TOOLS = new Set([
  'write_file', 'edit_file', 'patch_file', 'execute', 'execute_background',
])

const props = defineProps<{
  open: boolean
  agent: Agent | null
  conversationId: number | null | undefined
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  openWorkspaceSettings: []
}>()

const { t } = useI18n()
const MAX_TREE_DEPTH = 3

const workspaceDir = ref('')
const fileTree = ref<FileEntry[]>([])
const loading = ref(false)
const expandedDirs = ref<Set<string>>(new Set())

const sandboxMode = computed(() => props.agent?.sandbox_mode || 'codex')
const isSandbox = computed(() => sandboxMode.value === 'codex')
const hasConversation = computed(() => !!props.conversationId)

const defaultWorkDir = ref('')
const displayWorkDir = computed(() => props.agent?.work_dir || defaultWorkDir.value)

const refreshFileTree = async () => {
  if (!props.agent || !props.conversationId) return
  try {
    const files = await AgentsService.ListWorkspaceFiles(props.agent.id, props.conversationId)
    fileTree.value = files || []
  } catch {
    // Silently ignore refresh errors
  }
}

let refreshTimer: ReturnType<typeof setTimeout> | null = null

const debouncedRefresh = () => {
  if (!props.open) return
  if (refreshTimer) clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
    refreshTimer = null
    void refreshFileTree()
  }, 800)
}

const loadWorkspaceData = async () => {
  if (!props.agent || !props.conversationId) return
  loading.value = true
  try {
    const dir = await AgentsService.GetWorkspaceDir(props.agent.id, props.conversationId)
    workspaceDir.value = dir
    const files = await AgentsService.ListWorkspaceFiles(props.agent.id, props.conversationId)
    fileTree.value = files || []
  } catch (error) {
    console.error('Failed to load workspace data:', error)
    fileTree.value = []
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.open, props.agent?.id, props.conversationId],
  ([open]) => {
    if (open && props.agent) {
      void loadMCPServers()
      if (props.conversationId) {
        void loadWorkspaceData()
      }
    }
  },
  { immediate: true }
)

const toggleDir = (path: string) => {
  const newSet = new Set(expandedDirs.value)
  if (newSet.has(path)) {
    newSet.delete(path)
  } else {
    newSet.add(path)
  }
  expandedDirs.value = newSet
}

const handleOpenFolder = async () => {
  if (!hasConversation.value) {
    emit('openWorkspaceSettings')
    return
  }
  if (!workspaceDir.value) return
  try {
    await BrowserService.OpenDirectory(workspaceDir.value)
  } catch (error) {
    console.error('Failed to open directory:', error)
  }
}

const handleEnvironmentClick = () => {
  emit('openWorkspaceSettings')
}

const handleClose = () => {
  emit('update:open', false)
}

// ==================== MCP Tools ====================
const mcpServers = ref<MCPServer[]>([])
const mcpEnabled = computed(() => props.agent?.mcp_enabled ?? false)
const agentMCPServerIDs = computed<string[]>(() => {
  const raw = props.agent?.mcp_server_ids
  if (!raw || raw === '[]') return []
  try { return JSON.parse(raw) } catch { return [] }
})

async function loadMCPServers() {
  try {
    const all = await MCPService.ListServers()
    mcpServers.value = (all || []).filter((s) => s.enabled)
  } catch {
    mcpServers.value = []
  }
}

function isMCPServerSelected(id: string): boolean {
  return agentMCPServerIDs.value.includes(id)
}

async function handleMCPEnabledChange(val: boolean) {
  if (!props.agent) return
  try {
    await AgentsService.UpdateAgent(props.agent.id, new UpdateAgentInput({ mcp_enabled: val }))
    props.agent.mcp_enabled = val
  } catch (error) {
    console.error('Failed to update mcp_enabled:', error)
  }
}

async function handleMCPServerToggle(serverId: string, selected: boolean) {
  if (!props.agent) return
  const current = agentMCPServerIDs.value
  const updated = selected
    ? [...current, serverId]
    : current.filter((id) => id !== serverId)
  const json = JSON.stringify(updated)
  try {
    await AgentsService.UpdateAgent(props.agent.id, new UpdateAgentInput({ mcp_server_ids: json }))
    props.agent.mcp_server_ids = json
  } catch (error) {
    console.error('Failed to update mcp_server_ids:', error)
  }
}

// ==================== Add MCP Server Dialog ====================
const addDialogOpen = ref(false)
const addForm = ref({
  name: '',
  description: '',
  transport: 'stdio' as 'stdio' | 'streamableHttp',
  command: '',
  argsText: '',
  envPairs: [] as Array<{ key: string; value: string }>,
  url: '',
  headerPairs: [] as Array<{ key: string; value: string }>,
  timeout: 30,
})
const addSaving = ref(false)
const addTesting = ref(false)

function openAddMCPDialog() {
  addForm.value = {
    name: '',
    description: '',
    transport: 'stdio',
    command: '',
    argsText: '',
    envPairs: [],
    url: '',
    headerPairs: [],
    timeout: 30,
  }
  addDialogOpen.value = true
}

const canSaveAdd = computed(() => {
  const f = addForm.value
  if (!f.name.trim()) return false
  if (!f.description.trim()) return false
  if (f.transport === 'stdio' && !f.command.trim()) return false
  if (f.transport === 'streamableHttp' && !f.url.trim()) return false
  return true
})

function parseLinesToArray(text: string): string {
  const lines = text.split('\n').map((l) => l.trim()).filter(Boolean)
  return JSON.stringify(lines)
}

function pairsToJsonObject(pairs: Array<{ key: string; value: string }>): string {
  const obj: Record<string, string> = {}
  pairs.forEach(({ key, value }) => {
    const k = key.trim()
    if (k) obj[k] = value
  })
  return JSON.stringify(obj)
}

function addPair(pairs: Array<{ key: string; value: string }>) {
  pairs.push({ key: '', value: '' })
}

function removePair(pairs: Array<{ key: string; value: string }>, index: number) {
  pairs.splice(index, 1)
}

async function handleAddSave() {
  const form = addForm.value
  if (!form.name.trim() || !form.description.trim()) return

  const payload = {
    name: form.name.trim(),
    description: form.description.trim(),
    transport: form.transport,
    command: form.command.trim(),
    args: parseLinesToArray(form.argsText),
    env: pairsToJsonObject(form.envPairs),
    url: form.url.trim(),
    headers: pairsToJsonObject(form.headerPairs),
    timeout: form.timeout,
  }

  addTesting.value = true
  try {
    await MCPService.TestServer(payload)
  } catch (error) {
    toast.error(getErrorMessage(error) || t('settings.mcp.testFailed'))
    addTesting.value = false
    return
  }
  addTesting.value = false

  addSaving.value = true
  try {
    await MCPService.AddServer(payload)
    toast.success(t('settings.mcp.addSuccess'))
    addDialogOpen.value = false
    void loadMCPServers()
  } catch (error) {
    toast.error(getErrorMessage(error) || t('settings.mcp.addFailed'))
  } finally {
    addSaving.value = false
  }
}

let unsubTool: (() => void) | null = null
let unsubComplete: (() => void) | null = null

onMounted(() => {
  void AgentsService.GetDefaultWorkDir().then((dir) => {
    defaultWorkDir.value = dir
  })

  unsubTool = Events.On('chat:tool', (event: any) => {
    const data = Array.isArray(event?.data) ? event.data[0] : (event?.data ?? event)
    if (!data || !props.conversationId) return
    if (data.conversation_id !== props.conversationId) return
    if (data.type === 'result' && FS_MUTATING_TOOLS.has(data.tool_name)) {
      debouncedRefresh()
    }
  })

  unsubComplete = Events.On('chat:complete', (event: any) => {
    const data = Array.isArray(event?.data) ? event.data[0] : (event?.data ?? event)
    if (!data || !props.conversationId) return
    if (data.conversation_id !== props.conversationId) return
    debouncedRefresh()
  })
})

onUnmounted(() => {
  unsubTool?.()
  unsubTool = null
  unsubComplete?.()
  unsubComplete = null
  if (refreshTimer) {
    clearTimeout(refreshTimer)
    refreshTimer = null
  }
})
</script>

<template>
  <div
    :class="cn(
      'flex h-full shrink-0 flex-col border-l border-border bg-background transition-[width,opacity] duration-200 overflow-hidden',
      open ? 'w-[280px] opacity-100' : 'w-0 opacity-0 border-l-0'
    )"
  >
    <!-- Header -->
    <div class="flex items-center justify-between border-b border-border px-3 py-2">
      <span class="text-sm font-medium text-foreground">
        {{ t('assistant.workspaceDrawer.title') }}
      </span>
      <Button
        size="icon"
        variant="ghost"
        class="size-6"
        @click="handleClose"
      >
        <X class="size-3.5 text-muted-foreground" />
      </Button>
    </div>

    <!-- Content -->
    <div class="flex-1 overflow-y-auto px-3 py-3">
      <!-- Environment section -->
      <div class="mb-4">
        <div class="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {{ t('assistant.workspaceDrawer.environment') }}
        </div>
        <div class="flex gap-2">
          <button
            class="flex flex-1 items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors"
            :class="isSandbox
              ? 'border-primary bg-primary/10 text-primary'
              : 'border-border text-muted-foreground hover:border-foreground/20 hover:text-foreground'"
            @click="handleEnvironmentClick"
          >
            <ShieldCheck class="size-4" />
            <span class="truncate">{{ t('assistant.workspaceDrawer.sandboxEnv') }}</span>
          </button>
          <button
            class="flex flex-1 items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors"
            :class="!isSandbox
              ? 'border-primary bg-primary/10 text-primary'
              : 'border-border text-muted-foreground hover:border-foreground/20 hover:text-foreground'"
            @click="handleEnvironmentClick"
          >
            <Monitor class="size-4" />
            <span class="truncate">{{ t('assistant.workspaceDrawer.nativeEnv') }}</span>
          </button>
        </div>
      </div>

      <!-- Output files section -->
      <div>
        <div class="mb-2 flex items-center justify-between">
          <span class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ t('assistant.workspaceDrawer.outputFiles') }}
          </span>
          <TooltipProvider :delay-duration="300">
            <Tooltip>
              <TooltipTrigger as-child>
                <Button
                  size="icon"
                  variant="ghost"
                  class="size-5"
                  @click="handleOpenFolder"
                >
                  <FolderOpen class="size-3 text-muted-foreground" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="left">
                {{ hasConversation ? t('assistant.workspaceDrawer.openFolder') : t('assistant.workspaceDrawer.configureWorkspace') }}
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </div>

        <template v-if="hasConversation">
          <!-- Directory path -->
          <div
            v-if="workspaceDir"
            class="mb-2 truncate rounded-md bg-muted/50 px-2 py-1.5 font-mono text-[11px] text-muted-foreground"
            :title="workspaceDir"
          >
            {{ workspaceDir }}
          </div>

          <div class="mb-2 text-[11px] text-muted-foreground/80">
            {{ t('assistant.workspaceDrawer.depthLimitHint', { depth: MAX_TREE_DEPTH }) }}
          </div>

          <!-- File tree -->
          <div v-if="fileTree.length > 0" class="flex flex-col">
            <FileTreeNode
              v-for="entry in fileTree"
              :key="entry.path"
              :entry="entry"
              :depth="0"
              :expanded-dirs="expandedDirs"
              @toggle="toggleDir"
            />
          </div>

          <!-- Empty state -->
          <div
            v-else-if="!loading"
            class="flex items-center justify-center rounded-lg border border-dashed border-border py-6"
          >
            <span class="text-xs text-muted-foreground">
              {{ t('assistant.workspaceDrawer.noFiles') }}
            </span>
          </div>
        </template>

        <!-- No conversation: show default work dir with link to settings -->
        <button
          v-else
          class="group w-full cursor-pointer rounded-lg border border-dashed border-border px-3 py-3 text-left transition-colors hover:border-foreground/20 hover:bg-muted/50"
          @click="handleEnvironmentClick"
        >
          <div class="mb-1 text-[11px] text-muted-foreground">
            {{ t('assistant.workspaceDrawer.noConversationDir') }}
          </div>
          <div
            class="truncate font-mono text-[11px] text-muted-foreground"
            :title="displayWorkDir"
          >
            {{ displayWorkDir }}
          </div>
          <div class="mt-1.5 text-[11px] text-primary/70 group-hover:text-primary">
            {{ t('assistant.workspaceDrawer.noConversationAction') }}
          </div>
        </button>
      </div>

      <!-- MCP Tools section -->
      <div class="mt-4">
        <div class="mb-2 flex items-center justify-between">
          <span class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ t('assistant.workspaceDrawer.mcpTools') }}
          </span>
          <div class="flex items-center gap-1">
            <TooltipProvider :delay-duration="300">
              <Tooltip>
                <TooltipTrigger as-child>
                  <Button
                    size="icon"
                    variant="ghost"
                    class="size-5"
                    @click="openAddMCPDialog"
                  >
                    <Plus class="size-3 text-muted-foreground" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="left">
                  {{ t('settings.mcp.addServerTitle') }}
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <Switch
              :model-value="mcpEnabled"
              class="scale-75"
              @update:model-value="handleMCPEnabledChange"
            />
          </div>
        </div>

        <template v-if="mcpEnabled">
          <div v-if="mcpServers.length === 0" class="flex items-center justify-center rounded-lg border border-dashed border-border py-4">
            <span class="text-[11px] text-muted-foreground">
              {{ t('assistant.workspaceDrawer.mcpNoServers') }}
            </span>
          </div>
          <div v-else class="flex flex-col gap-1">
            <div
              v-for="server in mcpServers"
              :key="server.id"
              class="flex items-center justify-between gap-2 rounded-md px-2 py-1.5 transition-colors hover:bg-muted/50"
            >
              <div class="flex min-w-0 items-center gap-1.5">
                <Terminal v-if="server.transport === 'stdio'" class="size-3 shrink-0 text-muted-foreground" />
                <Globe v-else class="size-3 shrink-0 text-muted-foreground" />
                <span class="truncate text-xs text-foreground">{{ server.name }}</span>
              </div>
              <Switch
                :model-value="isMCPServerSelected(server.id)"
                class="scale-75"
                @update:model-value="(val: boolean) => handleMCPServerToggle(server.id, val)"
              />
            </div>
          </div>
        </template>
      </div>
    </div>

    <!-- Add MCP Server Dialog -->
    <Dialog v-model:open="addDialogOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t('settings.mcp.addServerTitle') }}</DialogTitle>
          <DialogDescription class="sr-only">{{ t('settings.mcp.addServerTitle') }}</DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-4 py-2">
          <div class="flex flex-col gap-1.5">
            <Label class="text-sm">{{ t('settings.mcp.serverName') }}</Label>
            <Input
              v-model="addForm.name"
              :placeholder="t('settings.mcp.serverNamePlaceholder')"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <div class="flex items-center justify-between">
              <Label class="text-sm">{{ t('settings.mcp.description') }}</Label>
              <span class="text-[10px] text-muted-foreground">{{ addForm.description.length }}/300</span>
            </div>
            <textarea
              v-model="addForm.description"
              :placeholder="t('settings.mcp.descriptionPlaceholder')"
              :maxlength="300"
              rows="2"
              class="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 resize-none"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <Label class="text-sm">{{ t('settings.mcp.transportType') }}</Label>
            <Select v-model="addForm.transport">
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="stdio">{{ t('settings.mcp.transportStdio') }}</SelectItem>
                <SelectItem value="streamableHttp">{{ t('settings.mcp.transportHttp') }}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <template v-if="addForm.transport === 'stdio'">
            <div class="flex flex-col gap-1.5">
              <Label class="text-sm">{{ t('settings.mcp.command') }}</Label>
              <Input
                v-model="addForm.command"
                :placeholder="t('settings.mcp.commandPlaceholder')"
              />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label class="text-sm">{{ t('settings.mcp.args') }}</Label>
              <textarea
                v-model="addForm.argsText"
                :placeholder="t('settings.mcp.argsPlaceholder')"
                rows="3"
                class="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              />
            </div>
            <div class="flex flex-col gap-1.5">
              <div class="flex items-center justify-between">
                <Label class="text-sm">{{ t('settings.mcp.envVars') }}</Label>
                <button
                  type="button"
                  class="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                  @click="addPair(addForm.envPairs)"
                >
                  <Plus class="size-3" />
                  {{ t('settings.mcp.addRow') }}
                </button>
              </div>
              <div v-if="addForm.envPairs.length === 0" class="text-xs text-muted-foreground py-1">
                {{ t('settings.mcp.envVarsPlaceholder') }}
              </div>
              <div
                v-for="(pair, idx) in addForm.envPairs"
                :key="idx"
                class="flex items-center gap-2"
              >
                <Input
                  v-model="pair.key"
                  placeholder="KEY"
                  class="flex-1 font-mono text-xs"
                />
                <span class="text-muted-foreground text-xs">=</span>
                <Input
                  v-model="pair.value"
                  placeholder="VALUE"
                  class="flex-1 font-mono text-xs"
                />
                <button
                  type="button"
                  class="shrink-0 rounded p-0.5 text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                  @click="removePair(addForm.envPairs, idx)"
                >
                  <X class="size-3.5" />
                </button>
              </div>
            </div>
          </template>

          <template v-if="addForm.transport === 'streamableHttp'">
            <div class="flex flex-col gap-1.5">
              <Label class="text-sm">{{ t('settings.mcp.serverUrl') }}</Label>
              <Input
                v-model="addForm.url"
                :placeholder="t('settings.mcp.serverUrlPlaceholder')"
              />
            </div>
            <div class="flex flex-col gap-1.5">
              <div class="flex items-center justify-between">
                <Label class="text-sm">{{ t('settings.mcp.httpHeaders') }}</Label>
                <button
                  type="button"
                  class="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                  @click="addPair(addForm.headerPairs)"
                >
                  <Plus class="size-3" />
                  {{ t('settings.mcp.addRow') }}
                </button>
              </div>
              <div v-if="addForm.headerPairs.length === 0" class="text-xs text-muted-foreground py-1">
                {{ t('settings.mcp.httpHeadersPlaceholder') }}
              </div>
              <div
                v-for="(pair, idx) in addForm.headerPairs"
                :key="idx"
                class="flex items-center gap-2"
              >
                <Input
                  v-model="pair.key"
                  placeholder="Header-Name"
                  class="flex-1 font-mono text-xs"
                />
                <span class="text-muted-foreground text-xs">:</span>
                <Input
                  v-model="pair.value"
                  placeholder="Value"
                  class="flex-1 font-mono text-xs"
                />
                <button
                  type="button"
                  class="shrink-0 rounded p-0.5 text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                  @click="removePair(addForm.headerPairs, idx)"
                >
                  <X class="size-3.5" />
                </button>
              </div>
            </div>
          </template>

          <div class="flex flex-col gap-1.5">
            <Label class="text-sm">{{ t('settings.mcp.timeout') }}</Label>
            <div class="flex items-center gap-2">
              <Input
                v-model.number="addForm.timeout"
                type="number"
                :min="1"
                :max="300"
                class="w-24"
              />
              <span class="text-xs text-muted-foreground">{{ t('settings.mcp.timeoutUnit') }}</span>
            </div>
          </div>
        </div>

        <DialogFooter>
          <button
            class="cursor-pointer rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-opacity hover:opacity-80 disabled:cursor-not-allowed disabled:opacity-50"
            :disabled="!canSaveAdd || addSaving || addTesting"
            @click="handleAddSave"
          >
            <Loader2 v-if="addTesting || addSaving" class="mr-1.5 inline size-3.5 animate-spin" />
            <template v-if="addTesting">{{ t('settings.mcp.testing') }}</template>
            <template v-else>{{ t('settings.mcp.addServer') }}</template>
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
