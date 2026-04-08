<script setup lang="ts">
import { computed } from 'vue'
import { MoreHorizontal, FolderPlus } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { cn } from '@/lib/utils'
import folderIcon from '@/assets/images/folder-icon.png'
import checkboxIconUrl from '@/assets/icons/checkbox-icon.svg?url'
import checkedIconUrl from '@/assets/icons/checked-icon.svg?url'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import IconRename from '@/assets/icons/library-rename.svg'
import IconDelete from '@/assets/icons/library-delete.svg'
import type { Folder as FolderType } from '@bindings/chatclaw/internal/services/library'

const { t } = useI18n()

const props = withDefaults(
  defineProps<{
    folder: FolderType
    // Total items inside this folder: sub-folders + documents
    documentCount?: number
    // Formatted latest updated time (e.g. 2026/03/02)
    latestUpdatedAt?: string
    selected?: boolean
    /** When > 1 and this card is selected, overflow menu shows batch actions (move / delete folder). */
    selectedCount?: number
  }>(),
  { selected: false, selectedCount: 0 }
)

const emit = defineEmits<{
  (e: 'click', folder: FolderType): void
  (e: 'rename', folder: FolderType): void
  (e: 'delete', folder: FolderType): void
  (e: 'move', folder: FolderType): void
  (e: 'toggle-select', folder: FolderType): void
  (e: 'batch-move-to-folder'): void
  (e: 'batch-delete'): void
}>()

const showBatchActions = computed(
  () => props.selected && props.selectedCount > 1
)

const handleCardClick = () => {
  emit('click', props.folder)
}
</script>

<template>
  <div
    :class="
      cn(
        'group relative flex h-[182px] w-[166px] cursor-pointer flex-col rounded-[12px] border border-border bg-card shadow-sm transition-[box-shadow,background-color] hover:bg-[#F5F5F5] hover:shadow-sm dark:border-white/15 dark:shadow-none dark:ring-1 dark:ring-white/5 dark:hover:bg-white/5 dark:hover:ring-white/10',
        selected && 'bg-[#F5F5F5] dark:bg-white/5 dark:ring-white/10'
      )
    "
    @click="handleCardClick"
  >
    <!-- Folder icon area: 6px radius, muted bg per design -->
    <div
      class="relative mx-2 mt-2 flex h-[86px] w-[150px] items-center justify-center overflow-hidden rounded-[6px] border border-border bg-[#f2f4f7] dark:border-border dark:bg-muted"
    >
      <img
        :src="folderIcon"
        alt=""
        class="h-auto w-[120px] max-w-full object-contain select-none"
        draggable="false"
      />
    </div>

    <!-- Selection + hover menu (aligned with DocumentCard) -->
    <div
      :class="
        cn(
          'absolute right-2 top-2 z-10 flex items-center gap-1 transition-opacity',
          selected ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'
        )
      "
    >
      <button
        type="button"
        :class="
          cn(
            'flex size-6 shrink-0 items-center justify-center overflow-visible rounded-[6px] text-white outline-none transition-colors focus-visible:ring-2 focus-visible:ring-white/40',
            selected && 'bg-[#404040] hover:bg-[#404040]'
          )
        "
        :aria-pressed="selected"
        @click.stop="emit('toggle-select', folder)"
      >
        <img
          v-if="!selected"
          :src="checkboxIconUrl"
          alt=""
          class="size-6 shrink-0 origin-center scale-[1.18] object-contain pointer-events-none select-none"
        />
        <img
          v-else
          :src="checkedIconUrl"
          alt=""
          class="size-6 shrink-0 object-contain pointer-events-none select-none"
        />
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger
          class="flex size-6 items-center justify-center rounded-[6px] bg-black/25 text-white/90 outline-none transition-colors hover:bg-black/35 hover:text-white focus-visible:ring-2 focus-visible:ring-white/40"
          @click.stop
        >
          <MoreHorizontal class="size-4 shrink-0" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" class="w-auto min-w-max">
          <template v-if="showBatchActions">
            <DropdownMenuItem class="gap-2 whitespace-nowrap" @select="emit('batch-move-to-folder')">
              <FolderPlus class="size-4 text-muted-foreground" />
              {{ t('knowledge.content.moveToFolder.title') }}
            </DropdownMenuItem>
            <DropdownMenuItem
              class="gap-2 whitespace-nowrap text-muted-foreground focus:text-foreground"
              @select="emit('batch-delete')"
            >
              <IconDelete class="size-4" />
              {{ t('knowledge.folder.delete') }}
            </DropdownMenuItem>
          </template>
          <template v-else>
            <DropdownMenuItem class="gap-2 whitespace-nowrap" @select="emit('rename', folder)">
              <IconRename class="size-4 text-muted-foreground" />
              {{ t('knowledge.folder.rename') }}
            </DropdownMenuItem>
            <DropdownMenuItem class="gap-2 whitespace-nowrap" @select="emit('move', folder)">
              <FolderPlus class="size-4 text-muted-foreground" />
              {{ t('knowledge.folder.move.action') }}
            </DropdownMenuItem>
            <DropdownMenuItem
              class="gap-2 whitespace-nowrap text-muted-foreground focus:text-foreground"
              @select="emit('delete', folder)"
            >
              <IconDelete class="size-4" />
              {{ t('knowledge.folder.delete') }}
            </DropdownMenuItem>
          </template>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>

    <!-- Title: 14px / 22px line-height per design -->
    <p
      class="mx-2 mt-2 line-clamp-2 h-[44px] break-all text-center text-sm font-medium leading-[22px] text-foreground"
      :title="folder.name"
    >
      {{ folder.name }}
    </p>

    <!-- Footer -->
    <div class="mx-2 mt-auto flex items-center justify-between pb-2">
      <div class="flex items-center gap-1 text-xs text-muted-foreground/70">
        <span v-if="documentCount !== undefined">{{ documentCount }}项</span>
        <span v-else>文件夹</span>
      </div>
      <div v-if="latestUpdatedAt" class="text-xs text-muted-foreground/60">
        {{ latestUpdatedAt }}
      </div>
    </div>
  </div>
</template>
