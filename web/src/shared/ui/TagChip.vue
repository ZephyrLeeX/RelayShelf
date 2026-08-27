<script setup lang="ts">
defineProps<{ name: string; color: string }>()
</script>
<template><span class="tag"><i :style="{ backgroundColor: color }" />{{ name }}</span></template>
<style scoped>
.tag { display:inline-flex; align-items:center; gap:.35rem; border:1px solid var(--border); border-radius:999px; padding:.18rem .5rem; font-size:.78rem; }
i { width:.55rem; height:.55rem; border-radius:50%; }
</style>
