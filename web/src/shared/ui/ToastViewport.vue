<script setup lang="ts">
import { CheckCircle2, CircleAlert, Info, TriangleAlert, X } from '@lucide/vue'
import { toast, type ToastType } from './toast'

const icons = { success: CheckCircle2, info: Info, warning: TriangleAlert, error: CircleAlert }
const labels: Record<ToastType, string> = { success: '成功', info: '提示', warning: '警告', error: '错误' }
const toasts = toast.items
</script>

<template>
  <div
    class="toast-viewport"
    aria-live="polite"
    aria-atomic="false"
  >
    <TransitionGroup name="toast">
      <div
        v-for="item in toasts"
        :key="item.id"
        class="toast-item"
        :class="item.type"
        :role="item.type === 'error' ? 'alert' : 'status'"
      >
        <component
          :is="icons[item.type]"
          class="toast-icon"
          aria-hidden="true"
        />
        <div>
          <strong>{{ labels[item.type] }}</strong>
          <p>{{ item.message }}</p>
        </div>
        <button
          type="button"
          :aria-label="`关闭${labels[item.type]}通知`"
          @click="toast.dismiss(item.id)"
        >
          <X aria-hidden="true" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.toast-viewport{position:fixed;z-index:100;right:max(1rem,env(safe-area-inset-right));bottom:max(1rem,env(safe-area-inset-bottom));display:grid;gap:.55rem;width:min(360px,calc(100vw - 2rem));pointer-events:none}.toast-item{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:start;gap:.65rem;padding:.75rem .8rem;border:1px solid var(--border-default);border-left:3px solid currentColor;border-radius:var(--radius-sm);background:color-mix(in srgb,var(--surface-raised) 96%,transparent);color:var(--text-secondary);box-shadow:var(--shadow-floating);backdrop-filter:blur(16px);pointer-events:auto}.toast-item.success{color:var(--state-success)}.toast-item.info{color:var(--accent-primary)}.toast-item.warning{color:var(--state-warning)}.toast-item.error{color:var(--state-danger)}.toast-icon{width:1.05rem;height:1.05rem;margin-top:.1rem}.toast-item strong{display:block;color:currentColor;font-size:.7rem}.toast-item p{margin:.15rem 0 0;overflow-wrap:anywhere;color:var(--text-primary);font-size:.78rem;line-height:1.4}.toast-item button{display:grid;place-items:center;width:28px;height:28px;margin:-.35rem -.4rem 0 0;padding:0;border:0;border-radius:.4rem;background:transparent;color:var(--text-tertiary)}.toast-item button:hover{background:var(--surface-soft);color:var(--text-primary)}.toast-item button svg{width:.9rem;height:.9rem}.toast-enter-active,.toast-leave-active{transition:opacity .16s ease,transform .16s ease}.toast-enter-from,.toast-leave-to{opacity:0;transform:translateY(8px)}
@media(max-width:1179px){.toast-viewport{bottom:calc(4.35rem + env(safe-area-inset-bottom))}}
@media(prefers-reduced-motion:reduce){.toast-enter-active,.toast-leave-active{transition:none}}
</style>
