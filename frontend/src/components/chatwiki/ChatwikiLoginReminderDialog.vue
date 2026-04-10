<script setup lang="ts">
import { nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { useNavigationStore, useSettingsStore } from '@/stores'
import { Events } from '@wailsio/runtime'
import {
  chatwikiLoginReminderOpen,
  onChatwikiLoginReminderOpenChange,
} from '@/composables/useChatwikiLoginReminderDialog'

const props = withDefaults(
  defineProps<{
    isSnapMode?: boolean
  }>(),
  {
    isSnapMode: false,
  }
)

const { t } = useI18n()
const navigationStore = useNavigationStore()
const settingsStore = useSettingsStore()

function handleOpenChange(next: boolean) {
  onChatwikiLoginReminderOpenChange(next)
}

async function goLoginNow() {
  onChatwikiLoginReminderOpenChange(false)
  await nextTick()
  if (props.isSnapMode) {
    Events.Emit('settings:open-chatwiki-login')
    return
  }
  settingsStore.requestChatwikiCloudLogin()
  settingsStore.setActiveMenu('chatwiki')
  navigationStore.navigateToModule('settings')
}

function goLoginLater() {
  onChatwikiLoginReminderOpenChange(false)
}
</script>

<template>
  <Dialog :open="chatwikiLoginReminderOpen" @update:open="handleOpenChange">
    <DialogContent
      size="md"
      :show-close-button="true"
      overlay-class="z-[60]"
      class="z-[60] w-[min(480px,calc(100vw-2rem))] max-w-[min(480px,calc(100vw-2rem))] gap-0 overflow-hidden rounded-[10px] border border-border bg-background p-0 shadow-sm dark:shadow-none dark:ring-1 dark:ring-white/10"
    >
      <div class="flex flex-col items-center px-6 pb-16 pt-16">
        <div
          class="mb-4 flex size-14 shrink-0 items-center justify-center overflow-hidden rounded-[10.5px] bg-muted/30"
        >
          <img src="@/assets/icons/chatclaw.svg?url" alt="" class="size-[51px] object-contain" />
        </div>
        <h2 class="text-center text-base font-semibold leading-6 text-foreground">ChatClaw</h2>
        <p class="mt-2 max-w-[320px] text-center text-sm leading-5 text-muted-foreground">
          {{ t('settings.chatwiki.loginReminder.subtitle') }}
        </p>
        <div class="mt-8 flex w-full max-w-[260px] flex-col gap-3">
          <Button
            type="button"
            class="h-10 w-full rounded-lg bg-foreground text-sm font-medium text-background hover:bg-foreground/90"
            @click="goLoginNow"
          >
            {{ t('settings.chatwiki.loginReminder.loginNow') }}
          </Button>
          <Button
            type="button"
            variant="secondary"
            class="h-10 w-full rounded-lg bg-muted text-sm font-medium text-foreground hover:bg-muted/80"
            @click="goLoginLater"
          >
            {{ t('settings.chatwiki.loginReminder.loginLater') }}
          </Button>
        </div>
        <p class="mt-8 max-w-[320px] text-center text-xs leading-4 text-muted-foreground/90">
          {{ t('settings.chatwiki.loginReminder.footer') }}
        </p>
      </div>
    </DialogContent>
  </Dialog>
</template>
