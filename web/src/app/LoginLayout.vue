<template>
  <main class="login-layout">
    <RouterView />
  </main>
</template>

<style scoped>
.login-layout { min-height:100vh; display:grid; place-items:center; padding:1.5rem; background:radial-gradient(circle at top, var(--accent-soft), var(--surface) 48%); }
</style>
