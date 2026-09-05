import { onMounted, onUnmounted, type Ref } from 'vue'

export function isFileDrag(event: DragEvent): boolean {
  return Array.from(event.dataTransfer?.types ?? []).includes('Files') ||
    Array.from(event.dataTransfer?.items ?? []).some((item) => item.kind === 'file')
}

/** One page-level counter keeps child transitions from clearing the overlay. */
export function useComposerFileDrop(dragging: Ref<boolean>, dropFiles: (event: DragEvent) => void) {
  let depth = 0
  function reset() {
    depth = 0
    dragging.value = false
  }
  function enter(event: DragEvent) {
    if (!isFileDrag(event)) return
    depth++
    dragging.value = true
  }
  function over(event: DragEvent) {
    if (!isFileDrag(event)) return
    event.preventDefault()
    dragging.value = true
  }
  function leave() {
    depth = Math.max(0, depth - 1)
    if (!depth) reset()
  }
  function drop(event: DragEvent) {
    if (isFileDrag(event)) event.preventDefault()
    reset()
  }
  function composerDrop(event: DragEvent) {
    if (!isFileDrag(event)) return
    event.preventDefault()
    reset()
    dropFiles(event)
  }
  onMounted(() => {
    window.addEventListener('dragenter', enter, true)
    window.addEventListener('dragover', over, true)
    window.addEventListener('dragleave', leave, true)
    window.addEventListener('drop', drop, true)
    window.addEventListener('dragend', reset, true)
    window.addEventListener('blur', reset)
  })
  onUnmounted(() => {
    window.removeEventListener('dragenter', enter, true)
    window.removeEventListener('dragover', over, true)
    window.removeEventListener('dragleave', leave, true)
    window.removeEventListener('drop', drop, true)
    window.removeEventListener('dragend', reset, true)
    window.removeEventListener('blur', reset)
    reset()
  })
  return composerDrop
}
