<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'

const props = defineProps<{ uri: string }>()
const canvas = ref<HTMLCanvasElement>()
const failed = ref(false)

async function draw(uri: string) {
  failed.value = false
  if (!uri.startsWith('otpauth://totp/')) {
    failed.value = true
    return
  }
  await nextTick()
  if (!canvas.value) return
  try {
    const { default: QRCode } = await import('qrcode')
    await QRCode.toCanvas(canvas.value, uri, {
      errorCorrectionLevel: 'M',
      margin: 2,
      width: 220,
      color: { dark: '#111827', light: '#ffffff' },
    })
  } catch {
    failed.value = true
  }
}

watch(() => props.uri, draw)
onMounted(() => draw(props.uri))
</script>

<template>
  <div class="totp-qr">
    <canvas
      ref="canvas"
      aria-label="TOTP enrollment 二维码"
      role="img"
    />
    <p
      v-if="failed"
      class="muted"
      role="status"
    >
      二维码生成失败，请使用下方密钥手动添加。
    </p>
  </div>
</template>

<style scoped>
.totp-qr{display:grid;justify-items:center;gap:.4rem}.totp-qr canvas{display:block;width:220px;height:220px;max-width:100%;border:1px solid var(--border-default);border-radius:var(--radius-sm);background:#fff}.totp-qr p{margin:0;font-size:.75rem}
</style>
