<script setup lang="ts">
import { useAuthStore } from '@/features/auth/store'
import { useTagsQuery } from '@/features/tags/queries'
import AccountButton from './AccountButton.vue'
import SidebarStatusCard from './SidebarStatusCard.vue'

defineProps<{ uploadCount: number, activeTransfers: boolean }>()
defineEmits<{ openUploads: [], openSessions: [], logout: [] }>()
const auth = useAuthStore()
const tags = useTagsQuery()
</script>

<template>
  <aside class="app-sidebar">
    <RouterLink
      class="brand"
      to="/temporary"
    >
      <img
        src="/favicon.svg"
        alt=""
        width="32"
        height="32"
      ><span><strong>RelayShelf</strong><small>CONTENT RELAY</small></span>
    </RouterLink>
    <nav
      class="main-nav"
      aria-label="主要区域"
    >
      <RouterLink to="/temporary">
        <span aria-hidden="true">◫</span>临时区
      </RouterLink>
      <RouterLink to="/permanent">
        <span aria-hidden="true">◆</span>长期区
      </RouterLink>
      <RouterLink to="/search">
        <span aria-hidden="true">⌕</span>搜索
      </RouterLink>
    </nav>
    <div class="library">
      <p class="nav-label">
        资料库
      </p>
      <nav aria-label="资料库">
        <RouterLink to="/favorites">
          收藏
        </RouterLink>
        <RouterLink to="/trash">
          回收站
        </RouterLink>
      </nav>
      <p class="nav-label">
        标签
      </p>
      <nav
        class="tags"
        aria-label="标签"
      >
        <RouterLink
          v-for="tag in tags.data.value"
          :key="tag.id"
          :to="`/tags/${tag.id}`"
        >
          <i :style="{ backgroundColor: tag.color }" />{{ tag.name }}
        </RouterLink>
        <span
          v-if="!tags.isPending.value && !tags.data.value?.length"
          class="empty"
        >尚无标签</span>
      </nav>
      <RouterLink
        v-if="auth.user?.isAdmin"
        class="admin"
        to="/admin"
      >
        管理
      </RouterLink>
    </div>
    <div class="sidebar-foot">
      <SidebarStatusCard
        :device="auth.device?.name"
        :upload-count="uploadCount"
        :active="activeTransfers"
        @open-uploads="$emit('openUploads')"
      />
      <AccountButton
        :name="auth.user?.displayName || auth.user?.username"
        @open="$emit('openSessions')"
      />
      <button
        class="logout"
        type="button"
        @click="$emit('logout')"
      >
        退出登录
      </button>
    </div>
  </aside>
</template>

<style scoped>
.app-sidebar{grid-row:1/-1;display:flex;flex-direction:column;min-height:100vh;padding:1rem .75rem .75rem;border-right:1px solid var(--border-default);background:var(--surface-raised)}
.brand{display:flex;align-items:center;gap:.65rem;margin:0 .35rem 1.45rem;text-decoration:none}.brand span{display:grid}.brand strong{font-size:1rem;letter-spacing:-.02em}.brand small{margin-top:.1rem;color:var(--text-tertiary);font:700 .55rem/1 var(--font-mono);letter-spacing:.12em}
nav{display:grid;gap:.2rem}.main-nav a,.library nav a,.admin{display:flex;align-items:center;gap:.65rem;min-height:40px;padding:.48rem .65rem;border-radius:var(--radius-sm);text-decoration:none;color:var(--text-secondary);font-size:.84rem}.main-nav a.router-link-active,.library nav a.router-link-active,.admin.router-link-active{background:var(--accent-primary-soft);color:var(--accent-primary-hover);font-weight:700}.main-nav span{width:1rem;text-align:center;color:var(--text-tertiary)}
.library{min-height:0;overflow:auto;margin-top:1.4rem}.nav-label{margin:.85rem .65rem .35rem;color:var(--text-tertiary);font:700 .62rem/1 var(--font-mono);letter-spacing:.12em;text-transform:uppercase}.tags i{width:.5rem;height:.5rem;border-radius:50%}.empty{display:block;padding:.5rem .65rem;color:var(--text-tertiary);font-size:.72rem}.admin{margin-top:.8rem}
.sidebar-foot{display:grid;gap:.35rem;margin-top:auto;padding-top:1rem}.logout{justify-self:start;margin-left:.45rem;border:0;background:transparent;color:var(--text-tertiary);font-size:.7rem}
</style>
