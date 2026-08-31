import { onBeforeUnmount, watch, type Ref } from 'vue'

/**
 * Closes a popover-style `open` flag on outside pointer interaction or Escape.
 * Listeners are bound only while open and are always cleaned up on unmount.
 */
export function useDismiss(open: Ref<boolean>, root: Ref<HTMLElement | undefined>) {
  function onPointerDown(event: PointerEvent) {
    const target = event.target
    if (target instanceof Node && root.value && !root.value.contains(target)) open.value = false
  }
  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') open.value = false
  }
  watch(open, (value) => {
    if (value) {
      document.addEventListener('pointerdown', onPointerDown, true)
      document.addEventListener('keydown', onKeydown, true)
    } else {
      document.removeEventListener('pointerdown', onPointerDown, true)
      document.removeEventListener('keydown', onKeydown, true)
    }
  })
  onBeforeUnmount(() => {
    document.removeEventListener('pointerdown', onPointerDown, true)
    document.removeEventListener('keydown', onKeydown, true)
  })
}
