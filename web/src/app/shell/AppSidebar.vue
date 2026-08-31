<script setup lang="ts">
import { Archive, Clock3, Search, Settings, Star, Trash2, Upload } from '@lucide/vue'
import type { StorageStatus } from '@/api/generated'
import type { RealtimeConnectionState } from '@/app/realtime'
import { useAuthStore } from '@/features/auth/store'
import { useTagsQuery } from '@/features/tags/queries'
import AccountButton from './AccountButton.vue'
import SidebarStatusCard from './SidebarStatusCard.vue'

defineProps<{
  uploadCount: number
  activeTransfers: boolean
  deviceCount?: number
  realtimeState: RealtimeConnectionState
  storage?: StorageStatus
}>()
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
      ><span><strong>RelayShelf</strong><small>内容中继</small></span>
    </RouterLink>
    <nav
      class="main-nav"
      aria-label="主要区域"
    >
      <RouterLink to="/temporary">
        <Clock3 aria-hidden="true" />临时区
      </RouterLink>
      <RouterLink to="/permanent">
        <Archive aria-hidden="true" />长期区
      </RouterLink>
      <RouterLink to="/favorites">
        <Star aria-hidden="true" />收藏
      </RouterLink>
      <RouterLink to="/search">
        <Search aria-hidden="true" />搜索
      </RouterLink>
    </nav>
    <div class="library">
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
      <nav
        class="tools"
        aria-label="工具"
      >
        <button
          type="button"
          @click="$emit('openUploads')"
        >
          <Upload aria-hidden="true" />上传<span
            v-if="uploadCount"
            class="badge"
          >{{ uploadCount }}</span>
        </button>
        <RouterLink to="/trash">
          <Trash2 aria-hidden="true" />回收站
        </RouterLink>
      </nav>
      <RouterLink
        v-if="auth.user?.isAdmin"
        class="admin"
        to="/admin"
      >
        <Settings aria-hidden="true" />管理
      </RouterLink>
    </div>
    <div class="sidebar-foot">
      <SidebarStatusCard
        :device="auth.device?.name"
        :device-count="deviceCount"
        :realtime-state="realtimeState"
        :storage="storage"
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
nav{display:grid;gap:.2rem}.main-nav a,.library nav a,.library nav button,.admin{display:flex;align-items:center;gap:.65rem;min-height:40px;padding:.48rem .65rem;border:0;border-radius:var(--radius-sm);background:transparent;text-decoration:none;text-align:left;color:var(--text-secondary);font-size:.84rem}.main-nav a.router-link-active,.library nav a.router-link-active,.admin.router-link-active{background:var(--accent-primary-soft);color:var(--accent-primary-hover);font-weight:700}.main-nav svg,.tools svg,.admin svg{width:1rem;height:1rem;flex:0 0 auto;color:var(--text-tertiary)}
.library{min-height:0;overflow:auto;margin-top:.7rem}.nav-label{margin:.85rem .65rem .35rem;color:var(--text-tertiary);font:700 .62rem/1 var(--font-mono);letter-spacing:.12em;text-transform:uppercase}.tags i{width:.5rem;height:.5rem;border-radius:50%}.empty{display:block;padding:.5rem .65rem;color:var(--text-tertiary);font-size:.72rem}.tools{margin-top:1rem;padding-top:1rem;border-top:1px solid var(--border-default)}.tools .badge{display:grid;place-items:center;width:1.25rem;height:1.25rem;margin-left:auto;border-radius:999px;background:var(--accent-primary-soft);color:var(--accent-primary);font-size:.65rem}.admin{margin-top:.8rem}
.sidebar-foot{display:grid;gap:.35rem;margin-top:auto;padding-top:1rem}.logout{justify-self:start;margin-left:.45rem;border:0;background:transparent;color:var(--text-tertiary);font-size:.7rem}
</style>
