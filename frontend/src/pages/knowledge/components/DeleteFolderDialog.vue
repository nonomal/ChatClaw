<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { LoaderCircle } from 'lucide-vue-next'
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
import { toast } from '@/components/ui/toast'
import { getErrorMessage } from '@/composables/useErrorMessage'
import { LibraryService, DeleteFolderInput } from '@bindings/chatclaw/internal/services/library'
import type { Folder } from '@bindings/chatclaw/internal/services/library'

const props = defineProps<{
  open: boolean
  folders: Folder[]
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  deleted: []
}>()

const { t } = useI18n()
const deleting = ref(false)

const close = (v: boolean) => emit('update:open', v)

const isBatch = computed(() => props.folders.length > 1)

const deleteDescription = computed(() => {
  if (props.folders.length === 0) return ''
  if (isBatch.value) {
    return t('knowledge.folder.deleteDescBatch', { count: props.folders.length })
  }
  return t('knowledge.folder.deleteDesc', { name: props.folders[0]?.name ?? '' })
})

watch(
  () => props.open,
  (open) => {
    if (!open) return
    deleting.value = false
  }
)

const handleDelete = async () => {
  if (props.folders.length === 0 || deleting.value) return
  deleting.value = true
  try {
    for (const folder of props.folders) {
      await LibraryService.DeleteFolder(
        new DeleteFolderInput({
          id: folder.id,
        })
      )
    }
    emit('deleted')
    const n = props.folders.length
    toast.success(
      n > 1 ? t('knowledge.folder.deleteSuccessBatch', { count: n }) : t('knowledge.folder.deleteSuccess')
    )
    close(false)
  } catch (error) {
    console.error('Failed to delete folder:', error)
    toast.error(getErrorMessage(error) || t('knowledge.folder.deleteFailed'))
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <AlertDialog :open="open" @update:open="close">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ t('knowledge.folder.deleteTitle') }}</AlertDialogTitle>
        <AlertDialogDescription>
          {{ deleteDescription }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel :disabled="deleting">
          {{ t('knowledge.folder.deleteCancel') }}
        </AlertDialogCancel>
        <AlertDialogAction
          class="inline-flex gap-2 bg-foreground text-background hover:bg-foreground/90"
          :disabled="deleting"
          @click.prevent="handleDelete"
        >
          <LoaderCircle v-if="deleting" class="size-4 shrink-0 animate-spin" />
          {{ t('knowledge.folder.deleteConfirm') }}
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
