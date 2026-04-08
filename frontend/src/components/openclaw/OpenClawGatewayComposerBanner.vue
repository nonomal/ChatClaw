<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { GatewayVisualStatus } from '@/stores/openclaw-gateway'
import { cn } from '@/lib/utils'

const props = defineProps<{
  visualStatus: GatewayVisualStatus
}>()

const { t } = useI18n()

const tagText = computed(() => {
  switch (props.visualStatus) {
    case GatewayVisualStatus.Error:
      return 'Gateway Error'
    case GatewayVisualStatus.Stop:
      return 'Gateway Stop'
    case GatewayVisualStatus.Starting:
      return 'Gateway Starting'
    case GatewayVisualStatus.Upgrading:
      return 'Gateway Upgrading'
    default:
      return 'Gateway Stop'
  }
})

const tagClass = computed(() =>
  cn(
    'inline-flex w-fit max-w-full shrink-0 items-center justify-center overflow-hidden rounded-md border border-solid px-[5px] text-sm font-normal leading-5',
    props.visualStatus === GatewayVisualStatus.Error &&
      'border-rose-300 text-rose-600 dark:border-rose-500/45 dark:text-rose-400',
    props.visualStatus === GatewayVisualStatus.Stop &&
      'border-neutral-300 text-neutral-600 dark:border-white/25 dark:text-neutral-400',
    (props.visualStatus === GatewayVisualStatus.Starting ||
      props.visualStatus === GatewayVisualStatus.Upgrading) &&
      'border-orange-300 text-amber-600 dark:border-amber-500/45 dark:text-amber-400'
  )
)
</script>

<template>
  <!-- Status tag only; disabled hint is shown as the textarea placeholder (no duplicate line). -->
  <div class="mb-3 flex w-full min-w-0 text-left">
    <span :class="tagClass">{{ tagText }}</span>
  </div>
</template>
