<script setup lang="ts">
/**
 * Read-only gateway status line (same layout as OpenClawGatewaySidebarStatus pill).
 * Used in sidebar button and SideNav system-switcher tooltip.
 */
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { Loader2 } from 'lucide-vue-next'
import {
  useOpenClawGatewayStore,
  gatewaySidebarTagShellClass,
  gatewaySidebarTagLabelClass,
  gatewaySidebarTagStatusClass,
  gatewaySidebarTagLoaderClass,
} from '@/stores'
import { GatewayVisualStatus } from '@/stores/openclaw-gateway'
import { cn } from '@/lib/utils'

const { t } = useI18n()
const gatewayStore = useOpenClawGatewayStore()
const { visualStatus } = storeToRefs(gatewayStore)

const badgeText = computed(() => t(`settings.openclawRuntime.statusBadge.${visualStatus.value}`))
const isStarting = computed(
  () =>
    visualStatus.value === GatewayVisualStatus.Starting ||
    visualStatus.value === GatewayVisualStatus.Upgrading
)

const labelSeparator = computed(() => t('settings.openclawRuntime.sidebarGatewayLabelSeparator'))

const v = computed(() => visualStatus.value)

const tagShellClass = computed(() => gatewaySidebarTagShellClass[v.value])
const tagLabelClass = computed(() => gatewaySidebarTagLabelClass[v.value])
const tagStatusClass = computed(() => gatewaySidebarTagStatusClass[v.value])
const tagLoaderClass = computed(() => gatewaySidebarTagLoaderClass[v.value])
</script>

<template>
  <div
    :class="
      cn(
        // Figma 1902:51640 — frame 60×20, text inset 5px horizontal, 6px radius, paragraph/small 14/20.
        'flex w-full min-w-0 flex-nowrap items-center justify-start gap-0 rounded-md border px-[5px] py-0 text-left text-sm leading-5',
        tagShellClass,
        'bg-white dark:bg-white'
      )
    "
    role="status"
    aria-live="polite"
  >
    <span class="inline-flex min-w-0 flex-1 items-center justify-start gap-1.5">
      <span :class="cn('min-w-0 truncate tabular-nums', tagStatusClass)">{{ badgeText }}</span>
      <Loader2 v-if="isStarting" :class="cn('size-3 shrink-0 animate-spin', tagLoaderClass)" />
    </span>
  </div>
</template>
