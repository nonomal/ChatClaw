import { ref } from 'vue'
import { getBinding as getChatwikiBinding } from '@/lib/chatwikiCache'
import {
  getChatwikiAvailabilityStatus,
  type ChatwikiAvailabilityStatus,
} from '@/lib/chatwikiModelAvailability'

const STORAGE_KEY = 'chatclaw_chatwiki_login_reminder_first_launch_v1'

export const chatwikiLoginReminderOpen = ref(false)

let firstLaunchSessionActive = false

export function openChatwikiLoginReminder(opts?: { firstLaunch?: boolean }) {
  if (opts?.firstLaunch) firstLaunchSessionActive = true
  chatwikiLoginReminderOpen.value = true
}

export function onChatwikiLoginReminderOpenChange(next: boolean) {
  chatwikiLoginReminderOpen.value = next
  if (!next && firstLaunchSessionActive) {
    try {
      localStorage.setItem(STORAGE_KEY, '1')
    } catch {
      /* ignore quota / private mode */
    }
    firstLaunchSessionActive = false
  }
}

function markFirstLaunchKeyIfLoggedIn(status: ChatwikiAvailabilityStatus) {
  if (status !== 'available') return
  try {
    localStorage.setItem(STORAGE_KEY, '1')
  } catch {
    /* ignore */
  }
}

/**
 * One-time prompt after install: show when ChatWiki cloud models are not yet available.
 * Skips when the user already has an active ChatWiki binding.
 */
export async function maybeShowFirstLaunchChatwikiLoginReminder() {
  if (typeof localStorage === 'undefined') return
  try {
    if (localStorage.getItem(STORAGE_KEY)) return
    const binding = await getChatwikiBinding()
    const status = getChatwikiAvailabilityStatus(binding)
    if (status === 'available') {
      markFirstLaunchKeyIfLoggedIn(status)
      return
    }
    openChatwikiLoginReminder({ firstLaunch: true })
  } catch {
    openChatwikiLoginReminder({ firstLaunch: true })
  }
}
