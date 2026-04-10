import { ref } from 'vue'

const STORAGE_KEY = 'chatclaw_chatwiki_login_reminder_first_launch_v1'

export const chatwikiLoginReminderOpen = ref(false)

export function openChatwikiLoginReminder(opts?: { firstLaunch?: boolean }) {
  if (opts?.firstLaunch) {
    try {
      localStorage.setItem(STORAGE_KEY, '1')
    } catch {
      /* ignore quota / private mode */
    }
  }
  chatwikiLoginReminderOpen.value = true
}

export function onChatwikiLoginReminderOpenChange(next: boolean) {
  chatwikiLoginReminderOpen.value = next
}

/**
 * One-time prompt after install: show once if the reminder key is not set.
 * The key is written when the dialog opens (see openChatwikiLoginReminder({ firstLaunch: true })).
 */
export function maybeShowFirstLaunchChatwikiLoginReminder() {
  if (typeof localStorage === 'undefined') return
  try {
    if (localStorage.getItem(STORAGE_KEY)) return
    openChatwikiLoginReminder({ firstLaunch: true })
  } catch {
    openChatwikiLoginReminder({ firstLaunch: true })
  }
}
