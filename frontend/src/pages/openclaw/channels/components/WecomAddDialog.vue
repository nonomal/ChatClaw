<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { LoaderCircle, QrCode, RefreshCw } from 'lucide-vue-next'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { toast, useToast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'
import {
  OpenClawChannelService,
  CreateChannelInput,
} from '@bindings/chatclaw/internal/services/openclaw/channels'
import { getPlatformDocsUrl, openExternalLink } from '@/pages/common/platformDocs'
import type { Channel } from '@bindings/chatclaw/internal/services/channels'

/** Poll interval: avoid high frequency (rate limits). */
const POLL_INTERVAL_MS = 2000
/** WeCom QR session validity (product requirement). */
const QR_VALID_MS = 5 * 60 * 1000

function qrImageUrlForAuth(authUrl: string) {
  return `https://api.qrserver.com/v1/create-qr-code/?size=220x220&margin=8&data=${encodeURIComponent(authUrl)}`
}

const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{
  saved: [channel: Channel, isEdit: boolean]
  manual: []
}>()

const { t } = useI18n()
const { toast: pushToast } = useToast()

type UiState = 'tips' | 'scan'
const uiState = ref<UiState>('tips')
const scode = ref('')
const authUrl = ref('')
const generating = ref(false)
const registering = ref(false)
/** Timestamp when current QR was issued (for 5-minute expiry). */
const qrIssuedAt = ref<number | null>(null)
const qrExpired = ref(false)

let pollTimer: ReturnType<typeof setInterval> | null = null

const qrImageSrc = computed(() => (authUrl.value ? qrImageUrlForAuth(authUrl.value) : ''))

function stopPolling() {
  if (pollTimer != null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function resetDialogBody() {
  stopPolling()
  uiState.value = 'tips'
  scode.value = ''
  authUrl.value = ''
  generating.value = false
  registering.value = false
  qrIssuedAt.value = null
  qrExpired.value = false
}

watch(open, (val) => {
  if (!val) {
    resetDialogBody()
  }
})

onUnmounted(() => {
  stopPolling()
})

async function fetchGenerateFromBackend() {
  const res = await OpenClawChannelService.WecomAuthQRGenerate()
  const s = res?.scode?.trim()
  const u = res?.auth_url?.trim()
  if (!s || !u) throw new Error('invalid response')
  return { scode: s, authUrl: u }
}

function extractBotCredentials(botInfo: unknown): { botId: string; secret: string } | null {
  if (!botInfo || typeof botInfo !== 'object') return null
  const o = botInfo as Record<string, unknown>
  const botId = String(o.botid ?? o.botId ?? o.bot_id ?? '').trim()
  const secret = String(o.secret ?? o.app_secret ?? '').trim()
  if (!botId || !secret) return null
  return { botId, secret }
}

async function registerChannel(botId: string, secret: string) {
  const name = t('channels.wecomAdd.defaultName')
  const extraConfig = JSON.stringify({
    platform: 'wecom',
    app_id: botId,
    app_secret: secret,
  })
  const channel = await OpenClawChannelService.CreateChannel(
    new CreateChannelInput({
      platform: 'wecom',
      name,
      avatar: '',
      extra_config: extraConfig,
      agent_id: 0,
    })
  )
  if (!channel) throw new Error('no channel')
  return channel
}

function isQrTimedOut(): boolean {
  if (qrIssuedAt.value == null) return false
  return Date.now() - qrIssuedAt.value > QR_VALID_MS
}

async function handlePollTick() {
  if (!scode.value || !open.value) return
  if (qrExpired.value) return

  if (isQrTimedOut()) {
    stopPolling()
    qrExpired.value = true
    pushToast({
      title: t('channels.wecomAdd.qrExpired'),
      description: t('channels.wecomAdd.qrExpiredHint'),
      variant: 'default',
      duration: 8000,
    })
    return
  }

  try {
    const data = await OpenClawChannelService.WecomAuthQRQuery(scode.value)
    if (!data) return
    const status = data.status
    if (status === 'success') {
      stopPolling()
      const creds = extractBotCredentials(data.bot_info)
      if (!creds) {
        toast.error(t('channels.wecomAdd.missingCredentials'))
        return
      }
      registering.value = true
      try {
        const channel = await registerChannel(creds.botId, creds.secret)
        open.value = false
        emit('saved', channel, false)
      } catch (e) {
        toast.error(getErrorMessage(e) || t('channels.wecomAdd.registerFailed'))
      } finally {
        registering.value = false
      }
      return
    }
    if (status === 'failed' || status === 'expired' || status === 'error') {
      stopPolling()
      toast.error(t('channels.wecomAdd.authFailed'))
    }
  } catch {
    // keep polling on transient errors
  }
}

function startPolling() {
  stopPolling()
  void handlePollTick()
  pollTimer = setInterval(() => {
    void handlePollTick()
  }, POLL_INTERVAL_MS)
}

async function handleGenerateQr() {
  generating.value = true
  qrExpired.value = false
  try {
    const { scode: s, authUrl: u } = await fetchGenerateFromBackend()
    scode.value = s
    authUrl.value = u
    qrIssuedAt.value = Date.now()
    uiState.value = 'scan'
    startPolling()
  } catch (e) {
    toast.error(getErrorMessage(e) || t('channels.wecomAdd.generateFailed'))
  } finally {
    generating.value = false
  }
}

async function handleRefreshQr() {
  await handleGenerateQr()
}

function handleManualEntry() {
  open.value = false
  emit('manual')
}

function openWecomConfigSteps() {
  void openExternalLink(getPlatformDocsUrl('wecom'))
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent class="max-w-[520px] gap-0 overflow-hidden p-0">
      <DialogHeader class="px-6 pt-6 pb-4">
        <DialogTitle class="text-lg font-semibold text-foreground">
          {{ t('channels.wecomAdd.title') }}
        </DialogTitle>
      </DialogHeader>

      <!-- Tips step: card + actions (aligned with WechatConfigDialog initial step) -->
      <div v-if="uiState === 'tips'" class="px-6 pb-6 space-y-5">
        <div
          class="rounded-lg border border-border bg-muted/50 p-4 space-y-1.5 text-sm text-foreground"
        >
          <p class="font-medium text-muted-foreground">{{ t('channels.wecomAdd.howTitle') }}</p>
          <p class="mt-2">
            {{ t('channels.wecomAdd.tipsIntro') }}
            <a
              :href="getPlatformDocsUrl('wecom')"
              target="_blank"
              rel="noopener noreferrer"
              class="ml-1 text-[#EF4444] underline underline-offset-2 hover:opacity-90"
              @click.prevent="openWecomConfigSteps"
            >
              {{ t('channels.inline.configSteps') }}
            </a>
          </p>
          <p class="mt-2 font-medium text-muted-foreground">
            {{ t('channels.wecomAdd.stepsLabel') }}
          </p>
          <ol class="mt-2 list-decimal space-y-2 pl-5 [list-style-position:outside]">
            <li>{{ t('channels.wecomAdd.step1') }}</li>
            <li>{{ t('channels.wecomAdd.step2') }}</li>
          </ol>
        </div>

        <div class="flex flex-wrap justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            class="h-10 gap-2 border-border px-4 shadow-sm dark:shadow-none dark:ring-1 dark:ring-white/10"
            :disabled="generating"
            @click="handleManualEntry"
          >
            {{ t('channels.wecomAdd.manualEntry') }}
          </Button>
          <Button
            type="button"
            class="h-10 gap-2 bg-foreground text-background hover:bg-foreground/90 dark:bg-primary dark:text-primary-foreground dark:hover:bg-primary/90"
            :disabled="generating"
            @click="handleGenerateQr"
          >
            <LoaderCircle v-if="generating" class="h-4 w-4 animate-spin" />
            <QrCode v-else class="h-4 w-4" />
            {{ generating ? t('channels.wecomAdd.generating') : t('channels.wecomAdd.generateQr') }}
          </Button>
        </div>
      </div>

      <!-- Scan step (aligned with WechatConfigDialog qrcode step) -->
      <div v-else class="px-6 pb-6 space-y-5">
        <p class="text-sm text-muted-foreground">{{ t('channels.wecomAdd.scanHint') }}</p>

        <div
          v-if="qrExpired"
          class="rounded-md border border-border border-l-[3px] border-l-muted-foreground bg-muted/30 px-3 py-2.5 text-center text-sm text-muted-foreground shadow-sm dark:shadow-none dark:ring-1 dark:ring-white/10"
        >
          {{ t('channels.wecomAdd.qrExpiredHint') }}
        </div>

        <div class="flex justify-center">
          <div
            class="relative flex h-[220px] w-[220px] items-center justify-center overflow-hidden rounded-xl border border-border bg-white shadow-sm dark:shadow-none dark:ring-1 dark:ring-white/10"
          >
            <img
              v-if="qrImageSrc"
              :src="qrImageSrc"
              alt=""
              class="h-full w-full object-contain p-3 transition-[filter,opacity] duration-200"
              :class="{ 'grayscale opacity-[0.42]': qrExpired }"
            />
            <div v-else class="flex h-full w-full items-center justify-center">
              <LoaderCircle class="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
            <div
              v-if="qrExpired && qrImageSrc"
              class="pointer-events-none absolute inset-0 flex items-center justify-center rounded-xl bg-background/55 dark:bg-background/50"
            >
              <span
                class="rounded-md border border-border bg-popover px-3 py-1.5 text-sm font-medium text-muted-foreground shadow-sm dark:shadow-none dark:ring-1 dark:ring-white/10"
              >
                {{ t('channels.wecomAdd.qrExpired') }}
              </span>
            </div>
          </div>
        </div>

        <div
          v-if="registering"
          class="flex items-center justify-center gap-2 text-xs text-muted-foreground"
        >
          <LoaderCircle class="h-3.5 w-3.5 shrink-0 animate-spin" />
          <span>{{ t('channels.wecomAdd.registering') }}</span>
        </div>

        <div class="flex justify-center">
          <Button
            type="button"
            variant="outline"
            class="h-10 gap-2 border-border px-6 shadow-sm dark:shadow-none dark:ring-1 dark:ring-white/10"
            :disabled="generating || registering"
            @click="handleRefreshQr"
          >
            <LoaderCircle v-if="generating" class="h-4 w-4 animate-spin" />
            <RefreshCw v-else class="h-4 w-4" />
            {{ t('channels.wecomAdd.refreshQr') }}
          </Button>
        </div>
      </div>
    </DialogContent>
  </Dialog>
</template>
