<script setup lang="ts">
/**
 * System switcher pill (ChatClaw / OpenClaw) — extracted for TooltipTrigger as-child + dropdown anchor.
 */
type SvgComponent = any
import { computed, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { Loader2 } from 'lucide-vue-next'
import {
  useNavigationStore,
  useAppStore,
  useOpenClawGatewayStore,
  GatewayVisualStatus,
  gatewaySwitcherDotClass,
  gatewaySidebarTagLoaderClass,
  type SystemOwner,
} from '@/stores'
import { cn } from '@/lib/utils'
import IconChatClaw from '@/assets/icons/chatclaw.svg'
import IconOpenClaw from '@/assets/icons/openclaw-logo.svg'
import IconDown from '@/assets/icons/down-icon.svg'

const props = defineProps<{
  /** Native title; omit when a rich tooltip covers gateway status (OpenClaw). */
  nativeTitle?: string
}>()

const { t } = useI18n()
const navigationStore = useNavigationStore()
const appStore = useAppStore()
const gatewayStore = useOpenClawGatewayStore()
const { visualStatus } = storeToRefs(gatewayStore)

const switcherGatewayDotClass = computed(() => gatewaySwitcherDotClass[visualStatus.value])
const showGatewayDotLoader = computed(
  () =>
    visualStatus.value === GatewayVisualStatus.Starting ||
    visualStatus.value === GatewayVisualStatus.Upgrading
)
const switcherGatewayLoaderClass = computed(
  () => gatewaySidebarTagLoaderClass[visualStatus.value]
)

interface SystemOption {
  value: SystemOwner
  labelKey: string
  icon: SvgComponent
}

const systemOptions: SystemOption[] = [
  { value: 'chatclaw', labelKey: 'nav.systemChatClaw', icon: IconChatClaw },
  { value: 'openclaw', labelKey: 'nav.systemOpenClaw', icon: IconOpenClaw },
]

const currentOption = computed(
  () => systemOptions.find((o) => o.value === appStore.currentSystem) ?? systemOptions[0]
)

const emit = defineEmits<{
  click: []
}>()

const buttonRef = ref<HTMLButtonElement | null>(null)
defineExpose({ buttonRef })
</script>

<template>
  <button
    ref="buttonRef"
    type="button"
    :class="
      cn(
        'flex w-full items-center rounded-[100px] border border-solid border-[#F5F5F5] bg-background px-2 py-[9px] text-[15px] font-bold transition-colors hover:border-[#d4d4d4] hover:bg-[#f0f0f0] dark:border-border dark:bg-muted/30 dark:hover:border-neutral-500 dark:hover:bg-muted/80',
        navigationStore.sidebarCollapsed ? 'justify-center px-1.5' : 'min-w-0 gap-3'
      )
    "
    :title="props.nativeTitle"
    @click="emit('click')"
  >
    <div
      :class="
        cn(
          'flex min-w-0 items-center',
          navigationStore.sidebarCollapsed
            ? cn('justify-center', appStore.currentSystem === 'openclaw' && 'gap-1.5')
            : 'min-w-0 flex-1 items-center gap-3'
        )
      "
    >
      <div
        :class="
          cn(
            'flex min-w-0 items-center gap-1.5',
            !navigationStore.sidebarCollapsed && 'min-w-0 flex-1'
          )
        "
      >
        <span
          class="inline-flex size-5 shrink-0 items-center justify-center overflow-hidden rounded-[3.75px]"
        >
          <component
            :is="currentOption.icon"
            class="block size-full min-h-0 min-w-0 max-h-full max-w-full shrink-0"
            preserveAspectRatio="xMidYMid meet"
            aria-hidden="true"
          />
        </span>
        <span
          v-if="!navigationStore.sidebarCollapsed"
          class="min-w-0 flex-1 truncate text-left text-[15px] font-bold leading-5 tracking-normal text-foreground"
        >
          {{ t(currentOption.labelKey) }}
        </span>
      </div>
      <Loader2
        v-if="appStore.currentSystem === 'openclaw' && showGatewayDotLoader"
        class="size-3 shrink-0 animate-spin"
        :class="switcherGatewayLoaderClass"
        aria-hidden="true"
      />
      <span
        v-else-if="appStore.currentSystem === 'openclaw'"
        class="size-2 shrink-0 rounded-full"
        :class="switcherGatewayDotClass"
        aria-hidden="true"
      />
    </div>
    <span
      v-if="!navigationStore.sidebarCollapsed"
      class="flex size-3.5 shrink-0 items-center justify-center text-muted-foreground"
    >
      <IconDown class="size-3.5" />
    </span>
  </button>
</template>
