<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { DefaultService, type AdminUser, type HealthState, type UpdateRuntimeSettingsRequest } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { displayError } from '@/shared/api/errors'
import { bytesToUnit, formatBytes, unitToBytes } from '@/shared/utils/bytes'

type Section = 'overview' | 'storage' | 'settings' | 'users'
type UserAction = 'disable' | 'reset' | 'delete'
const USER_PAGE_SIZE = 30
const section = ref<Section>('overview')
const client = useQueryClient()
const error = ref('')
const notice = ref('')
const status = useQuery({ queryKey: queryKeys.admin.status(), queryFn: () => DefaultService.getAdminStatus(), refetchInterval: 30_000 })
const storage = useQuery({ queryKey: queryKeys.admin.storage(), queryFn: () => DefaultService.getStorageStatus(), enabled: computed(() => section.value === 'storage') })
const settings = useQuery({ queryKey: queryKeys.admin.settings(), queryFn: () => DefaultService.getRuntimeSettings(), enabled: computed(() => section.value === 'settings') })
const users = useInfiniteQuery({
  queryKey: queryKeys.admin.users(),
  enabled: computed(() => section.value === 'users'),
  initialPageParam: undefined as string | undefined,
  queryFn: ({ pageParam }) => DefaultService.listAdminUsers(pageParam, USER_PAGE_SIZE),
  getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
})
const userRows = computed(() => users.data.value?.pages.flatMap((page) => page.items) ?? [])

const MEBIBYTE = 1024 ** 2
const GIBIBYTE = 1024 ** 3
const settingsForm = reactive({ temporaryTtlHours: 72, trashTtlHours: 168, maxFileSizeMB: 2048, maxStorageGB: '' as number | '', auditRetentionDays: 90, uploadRetentionHours: 24 })
watch(() => settings.data.value, (value) => {
  if (!value) return
  Object.assign(settingsForm, {
    temporaryTtlHours: value.temporaryTtlHours,
    trashTtlHours: value.trashTtlHours,
    maxFileSizeMB: bytesToUnit(value.maxFileSizeBytes, MEBIBYTE),
    maxStorageGB: value.maxStorageBytes == null ? '' : bytesToUnit(value.maxStorageBytes, GIBIBYTE),
    auditRetentionDays: value.auditRetentionDays,
    uploadRetentionHours: value.uploadRetentionHours,
  })
}, { immediate: true })

const saveSettings = useMutation({
  mutationFn: () => DefaultService.updateRuntimeSettings({
    temporaryTtlHours: Number(settingsForm.temporaryTtlHours), trashTtlHours: Number(settingsForm.trashTtlHours),
    maxFileSizeBytes: unitToBytes(settingsForm.maxFileSizeMB, MEBIBYTE), maxStorageBytes: settingsForm.maxStorageGB === '' ? null : unitToBytes(settingsForm.maxStorageGB, GIBIBYTE),
    auditRetentionDays: Number(settingsForm.auditRetentionDays), uploadRetentionHours: Number(settingsForm.uploadRetentionHours),
  } satisfies UpdateRuntimeSettingsRequest),
  onSuccess: (value) => { client.setQueryData(queryKeys.admin.settings(), value); notice.value = '运行时设置已保存'; error.value = ''; void client.invalidateQueries({ queryKey: queryKeys.admin.status() }) },
  onError: (cause) => { error.value = displayError(cause) },
})

const createForm = reactive({ username: '', displayName: '', password: '', isAdmin: false })
const createUser = useMutation({
  mutationFn: () => DefaultService.createAdminUser({ ...createForm }),
  onSuccess: () => { Object.assign(createForm, { username: '', displayName: '', password: '', isAdmin: false }); notice.value = '用户已创建'; error.value = ''; void client.invalidateQueries({ queryKey: queryKeys.admin.users() }) },
  onError: (cause) => { error.value = displayError(cause) },
})

const pending = ref<{ action: UserAction, user: AdminUser } | null>(null)
const resetPassword = ref('')
const deletePhrase = ref('')
const userMutation = useMutation({
  mutationFn: async () => {
    if (!pending.value) return
    const { action, user } = pending.value
    if (action === 'disable') await DefaultService.disableAdminUser(user.id)
    if (action === 'reset') await DefaultService.resetAdminUserPassword(user.id, { newPassword: resetPassword.value })
    if (action === 'delete') await DefaultService.deleteAdminUser(user.id)
  },
  onSuccess: () => { notice.value = pending.value?.action === 'delete' ? '用户已永久删除' : '用户安全状态已更新'; error.value = ''; closeDialog(); void client.invalidateQueries({ queryKey: queryKeys.admin.users() }); void client.invalidateQueries({ queryKey: queryKeys.admin.status() }) },
  onError: (cause) => { error.value = displayError(cause) },
})

function openAction(action: UserAction, user: AdminUser) { pending.value = { action, user }; resetPassword.value = ''; deletePhrase.value = ''; error.value = '' }
function closeDialog() { pending.value = null; resetPassword.value = ''; deletePhrase.value = '' }
const actionAllowed = computed(() => {
  if (!pending.value) return false
  if (pending.value.action === 'reset') return resetPassword.value.length >= 10
  if (pending.value.action === 'delete') return deletePhrase.value === pending.value.user.username
  return true
})
function stateLabel(value?: HealthState) { return value === 'HEALTHY' ? '正常' : value === 'DEGRADED' ? '降级' : '不可用' }
function thresholdLabel(value: string) {
  return ({ UNCONFIGURED: '未配置', NORMAL: '正常', WARNING: '接近上限', STRONG_WARNING: '严重接近上限', LIMIT_REACHED: '已达到上限' } as Record<string, string>)[value] ?? value
}
function userStatusLabel(value: string) { return value === 'ACTIVE' ? '正常' : '已禁用' }
function degradedReasonLabel(value: string) {
  return ({
    NAS_UNAVAILABLE: 'NAS 不可用', NAS_TIMEOUT: 'NAS 响应超时', NAS_FULL: 'NAS 空间已满', LOGICAL_THRESHOLD_WARNING: '逻辑容量接近阈值',
    LOGICAL_THRESHOLD_EXCEEDED: '逻辑容量已超出阈值', STAGING_UNAVAILABLE: '暂存空间不可用', DATABASE_UNAVAILABLE: '数据库不可用',
  } as Record<string, string>)[value] ?? value
}
function shortCommit(value: string) { return value === 'unknown' ? value : value.slice(0, 12) }
function percent(used: number, total?: number | null) { return total ? Math.min(100, Math.round(used / total * 100)) : 0 }
</script>

<template>
  <section class="admin-workspace">
    <header class="admin-head">
      <div>
        <p class="eyebrow">
          运行控制
        </p><h1>系统管理</h1><p class="muted">
          维护账号、容量和运行参数。私人内容不在此处出现。
        </p>
      </div>
      <span
        class="overall"
        :data-state="status.data.value?.state || 'UNAVAILABLE'"
      ><i />{{ stateLabel(status.data.value?.state) }}</span>
    </header>
    <nav
      class="admin-tabs"
      aria-label="管理区域"
    >
      <button
        v-for="item in ([['overview','概览'],['storage','存储'],['settings','设置'],['users','用户']] as const)"
        :key="item[0]"
        :class="{ active: section === item[0] }"
        @click="section = item[0]"
      >
        {{ item[1] }}
      </button>
    </nav>
    <p
      v-if="error"
      class="callout error"
      role="alert"
    >
      {{ error }}
    </p><p
      v-if="notice"
      class="callout success"
      role="status"
    >
      {{ notice }}
    </p>

    <div
      v-if="section === 'overview'"
      class="view-stack"
    >
      <p
        v-if="status.isPending.value"
        class="panel loading"
      >
        正在读取运行状态…
      </p>
      <p
        v-else-if="status.isError.value"
        class="panel loading error"
      >
        {{ displayError(status.error.value) }}
      </p>
      <template v-else-if="status.data.value">
        <section
          class="status-rail panel"
          aria-label="依赖状态"
        >
          <article><span :data-state="status.data.value.databaseState" /><div><small>PostgreSQL</small><strong>{{ stateLabel(status.data.value.databaseState) }}</strong></div></article>
          <article><span :data-state="status.data.value.storage.state" /><div><small>NAS / 暂存空间</small><strong>{{ stateLabel(status.data.value.storage.state) }}</strong></div></article>
          <article><span :data-state="status.data.value.migration.compatible ? 'HEALTHY' : 'DEGRADED'" /><div><small>数据库结构</small><strong>{{ status.data.value.migration.currentVersion }} / {{ status.data.value.migration.latestVersion }}</strong></div></article>
          <article><span :data-state="status.data.value.security.adminTotpSatisfied ? 'HEALTHY' : 'DEGRADED'" /><div><small>管理员 TOTP</small><strong>{{ status.data.value.security.adminTotpSatisfied ? '满足' : `${status.data.value.security.activeAdminsWithoutTOTP} 名未启用` }}</strong></div></article>
        </section>
        <div class="overview-grid">
          <section class="panel metric">
            <small>逻辑存储</small><strong>{{ formatBytes(status.data.value.storage.logicalUsageBytes) }}</strong><p>{{ thresholdLabel(status.data.value.storage.thresholdState) }}</p>
          </section>
          <section class="panel metric">
            <small>失败任务</small><strong>{{ status.data.value.failedJobs.length }}</strong><p>仅显示安全诊断元数据</p>
          </section>
          <section class="panel build">
            <h2>构建</h2><dl>
              <div><dt>版本</dt><dd>{{ status.data.value.build.version }}</dd></div><div>
                <dt>提交</dt><dd class="mono">
                  {{ shortCommit(status.data.value.build.gitCommit) }}
                </dd>
              </div><div><dt>构建时间</dt><dd>{{ status.data.value.build.buildTime }}</dd></div>
            </dl>
          </section>
        </div>
        <section class="panel jobs">
          <header><h2>失败任务</h2><span>{{ status.data.value.failedJobs.length }}/50</span></header><p
            v-if="!status.data.value.failedJobs.length"
            class="empty"
          >
            当前没有失败任务。
          </p><ul v-else>
            <li
              v-for="job in status.data.value.failedJobs"
              :key="job.id"
            >
              <span><strong>{{ job.type }}</strong><small>{{ job.subjectType }} · {{ new Date(job.updatedAt).toLocaleString() }}</small></span><code>{{ job.errorCode }}</code>
            </li>
          </ul>
        </section>
      </template>
    </div>

    <div
      v-if="section === 'storage'"
      class="view-stack"
    >
      <p
        v-if="storage.isPending.value"
        class="panel loading"
      >
        正在检查存储状态…
      </p>
      <template v-else-if="storage.data.value">
        <section class="panel capacity">
          <header>
            <div>
              <p class="eyebrow">
                去重对象
              </p><h2>逻辑容量</h2>
            </div><strong>{{ formatBytes(storage.data.value.logicalUsageBytes) }}</strong>
          </header><div class="meter">
            <i :style="{ width: `${percent(storage.data.value.logicalUsageBytes, storage.data.value.maxStorageBytes)}%` }" />
          </div><p class="muted">
            阈值 {{ storage.data.value.maxStorageBytes ? formatBytes(storage.data.value.maxStorageBytes) : '未配置' }} · {{ thresholdLabel(storage.data.value.thresholdState) }}
          </p>
        </section>
        <div class="storage-grid">
          <section class="panel metric">
            <small>NAS 实际可用</small><strong>{{ storage.data.value.nasAvailableBytes == null ? '不可用' : formatBytes(storage.data.value.nasAvailableBytes) }}</strong><p>总计 {{ storage.data.value.nasTotalBytes == null ? '—' : formatBytes(storage.data.value.nasTotalBytes) }}</p>
          </section><section class="panel metric">
            <small>VM 暂存占用</small><strong>{{ formatBytes(storage.data.value.stagingUsageBytes) }}</strong><p>磁盘可用 {{ storage.data.value.stagingAvailableBytes == null ? '—' : formatBytes(storage.data.value.stagingAvailableBytes) }}</p>
          </section>
        </div>
        <section
          v-if="storage.data.value.degradedReasons.length"
          class="panel degraded"
        >
          <h2>降级原因</h2><ul>
            <li
              v-for="reason in storage.data.value.degradedReasons"
              :key="reason"
            >
              {{ degradedReasonLabel(reason) }}
            </li>
          </ul>
        </section>
      </template>
    </div>

    <form
      v-if="section === 'settings'"
      class="panel settings-form"
      @submit.prevent="saveSettings.mutate()"
    >
      <header>
        <div>
          <p class="eyebrow">
            数据库存储
          </p><h2>运行时设置</h2>
        </div><p class="muted">
          部署路径、密钥和并发参数只能在进程启动时配置。
        </p>
      </header>
      <p v-if="settings.isPending.value">
        正在读取设置…
      </p>
      <div
        v-else
        class="form-grid"
      >
        <label class="field">临时内容保留（小时）<input
          v-model.number="settingsForm.temporaryTtlHours"
          type="number"
          min="1"
          max="8760"
          required
        ></label><label class="field">回收站保留（小时）<input
          v-model.number="settingsForm.trashTtlHours"
          type="number"
          min="1"
          max="8760"
          required
        ></label><label class="field">单文件上限（MB）<input
          v-model.number="settingsForm.maxFileSizeMB"
          type="number"
          min="0.01"
          max="2097152"
          step="0.01"
          required
        ><small>当前约 {{ formatBytes(unitToBytes(settingsForm.maxFileSizeMB, MEBIBYTE)) }}</small></label><label class="field">逻辑容量阈值（GB，可空）<input
          v-model.number="settingsForm.maxStorageGB"
          type="number"
          min="0.01"
          step="0.01"
        ><small v-if="settingsForm.maxStorageGB !== ''">当前约 {{ formatBytes(unitToBytes(settingsForm.maxStorageGB, GIBIBYTE)) }}</small></label><label class="field">审计保留（天）<input
          v-model.number="settingsForm.auditRetentionDays"
          type="number"
          min="1"
          max="3650"
          required
        ></label><label class="field">未完成上传保留（小时）<input
          v-model.number="settingsForm.uploadRetentionHours"
          type="number"
          min="1"
          max="168"
          required
        ></label>
      </div>
      <footer>
        <span class="muted">保存后只影响新生成的生命周期截止时间。</span><button
          class="button primary"
          :disabled="saveSettings.isPending.value"
        >
          保存设置
        </button>
      </footer>
    </form>

    <div
      v-if="section === 'users'"
      class="users-layout"
    >
      <form
        class="panel create-user"
        @submit.prevent="createUser.mutate()"
      >
        <p class="eyebrow">
          仅限管理员创建
        </p><h2>创建用户</h2><label class="field">用户名<input
          v-model="createForm.username"
          maxlength="64"
          autocomplete="off"
          required
        ></label><label class="field">显示名称<input
          v-model="createForm.displayName"
          maxlength="100"
          required
        ></label><label class="field">初始密码<input
          v-model="createForm.password"
          type="password"
          minlength="10"
          maxlength="1024"
          autocomplete="new-password"
          required
        ></label><label class="check"><input
          v-model="createForm.isAdmin"
          type="checkbox"
        >授予管理员权限</label><button
          class="button primary"
          :disabled="createUser.isPending.value"
        >
          创建用户
        </button>
      </form>
      <section class="panel user-list">
        <header>
          <div>
            <p class="eyebrow">
              仅显示运行元数据
            </p><h2>用户</h2>
          </div><span>已加载 {{ userRows.length }}</span>
        </header><p v-if="users.isPending.value">
          正在读取用户…
        </p><p
          v-else-if="!userRows.length"
          class="empty"
        >
          尚无用户。
        </p><template v-else>
          <ul>
            <li
              v-for="user in userRows"
              :key="user.id"
            >
              <div class="identity">
                <span class="avatar">{{ (user.displayName || user.username).slice(0,1).toUpperCase() }}</span><span><strong>{{ user.displayName }}</strong><small>@{{ user.username }} · {{ user.isAdmin ? '管理员' : '普通用户' }}</small></span>
              </div><span
                class="state"
                :data-active="user.status === 'ACTIVE'"
              >{{ userStatusLabel(user.status) }}</span><div class="actions">
                <button
                  class="button"
                  :disabled="user.status === 'DISABLED'"
                  @click="openAction('disable',user)"
                >
                  禁用
                </button><button
                  class="button"
                  @click="openAction('reset',user)"
                >
                  重置密码
                </button><button
                  class="button danger"
                  @click="openAction('delete',user)"
                >
                  删除
                </button>
              </div>
            </li>
          </ul>
          <footer
            v-if="users.hasNextPage.value"
            class="load-more"
          >
            <button
              class="button"
              :disabled="users.isFetchingNextPage.value"
              @click="users.fetchNextPage()"
            >
              {{ users.isFetchingNextPage.value ? '正在加载…' : '加载更多用户' }}
            </button>
          </footer>
        </template>
      </section>
    </div>

    <Teleport to="body">
      <div
        v-if="pending"
        class="dialog-backdrop"
        @click.self="closeDialog"
      >
        <section
          class="panel confirm"
          role="dialog"
          aria-modal="true"
          aria-labelledby="confirm-title"
        >
          <p class="eyebrow">
            高风险操作
          </p><h2 id="confirm-title">
            {{ pending.action === 'disable' ? '禁用用户' : pending.action === 'reset' ? '重置密码' : '永久删除用户' }}
          </h2><p>目标：<strong>{{ pending.user.displayName }}</strong>（@{{ pending.user.username }}）</p><p
            v-if="pending.action === 'disable'"
            class="muted"
          >
            该用户的所有现有会话将立即撤销。
          </p><label
            v-if="pending.action === 'reset'"
            class="field"
          >新密码<input
            v-model="resetPassword"
            type="password"
            minlength="10"
            autocomplete="new-password"
          ></label><label
            v-if="pending.action === 'delete'"
            class="field"
          >输入用户名 <strong>{{ pending.user.username }}</strong> 确认<input
            v-model="deletePhrase"
            autocomplete="off"
          ></label><footer>
            <button
              class="button"
              @click="closeDialog"
            >
              取消
            </button><button
              class="button danger"
              :disabled="!actionAllowed || userMutation.isPending.value"
              @click="userMutation.mutate()"
            >
              确认{{ pending.action === 'delete' ? '永久删除' : '操作' }}
            </button>
          </footer>
        </section>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.admin-workspace{display:grid;gap:1.2rem}.admin-head{display:flex;justify-content:space-between;align-items:flex-end;gap:1rem;padding:.4rem 0}.admin-head h1{margin:.1rem 0;font-size:clamp(1.8rem,4vw,3rem);letter-spacing:-.045em}.eyebrow{margin:0;color:var(--accent-strong);font:700 .7rem/1 var(--font-mono);letter-spacing:.12em;text-transform:uppercase}.overall{display:flex;align-items:center;gap:.55rem;padding:.55rem .8rem;border:1px solid var(--border);border-radius:999px;background:var(--surface-raised);font-weight:700}.overall i,.status-rail article>span{width:.65rem;height:.65rem;border-radius:50%;background:var(--danger)}[data-state="HEALTHY"] i,.status-rail [data-state="HEALTHY"]{background:var(--accent)}[data-state="DEGRADED"] i,.status-rail [data-state="DEGRADED"]{background:#d69722}.admin-tabs{display:flex;gap:.25rem;padding:.3rem;border:1px solid var(--border);border-radius:var(--radius);background:var(--surface-soft);width:max-content}.admin-tabs button{border:0;border-radius:calc(var(--radius) - .25rem);padding:.55rem .9rem;background:transparent}.admin-tabs button.active{background:var(--surface-raised);box-shadow:0 1px 4px rgb(0 0 0/.08);font-weight:700}.view-stack{display:grid;gap:1rem}.loading,.settings-form,.create-user{padding:1.2rem}.status-rail{display:grid;grid-template-columns:repeat(3,1fr);padding:1rem}.status-rail article{display:flex;align-items:center;gap:.7rem;padding:.25rem 1rem;border-right:1px solid var(--border)}.status-rail article:last-child{border:0}.status-rail small,.status-rail strong{display:block}.status-rail small,.metric small{color:var(--muted)}.overview-grid,.storage-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:1rem}.overview-grid .build{grid-column:span 2}.metric,.build,.jobs,.capacity,.degraded{padding:1.2rem}.metric strong{display:block;margin:.35rem 0;font-size:1.8rem;letter-spacing:-.04em}.metric p{margin:0;color:var(--muted);font-size:.85rem}.build h2,.jobs h2,.capacity h2,.degraded h2,.settings-form h2,.create-user h2,.user-list h2{margin:.2rem 0}.build dl{display:grid;grid-template-columns:repeat(3,1fr);gap:1rem}.build dl div{border-left:2px solid var(--accent-soft);padding-left:.75rem}.build dt{color:var(--muted);font-size:.75rem}.build dd{margin:.3rem 0 0}.mono,code{font-family:var(--font-mono)}.jobs header,.capacity header,.settings-form header,.user-list header{display:flex;justify-content:space-between;gap:1rem;align-items:flex-start}.jobs ul,.user-list ul,.degraded ul{list-style:none;padding:0;margin:1rem 0 0}.jobs li{display:flex;justify-content:space-between;gap:1rem;padding:.8rem 0;border-top:1px solid var(--border)}.jobs small,.identity small{display:block;color:var(--muted);margin-top:.2rem}.empty{padding:1.2rem 0;color:var(--muted)}.capacity strong{font-size:2rem}.meter{height:.6rem;border-radius:999px;background:var(--surface-soft);overflow:hidden;margin:1rem 0}.meter i{display:block;height:100%;background:var(--accent);border-radius:inherit}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:1rem;margin:1.4rem 0}.settings-form footer,.confirm footer{display:flex;justify-content:space-between;align-items:center;gap:1rem;border-top:1px solid var(--border);padding-top:1rem}.users-layout{display:grid;grid-template-columns:minmax(220px,280px) minmax(0,1fr);gap:1rem;align-items:start}.create-user{display:grid;gap:.85rem;position:sticky;top:90px}.check{display:flex;gap:.55rem;align-items:center}.user-list{padding:1.2rem}.user-list li{display:grid;grid-template-columns:minmax(170px,1fr) auto;gap:.75rem;padding:1rem 0;border-top:1px solid var(--border)}.load-more{display:flex;justify-content:center;padding:.9rem 0 0}.identity{display:flex;gap:.65rem;align-items:center}.avatar{display:grid;place-items:center;width:2.35rem;height:2.35rem;border-radius:.65rem;background:var(--accent-soft);color:var(--accent-strong);font-weight:800}.state{font:700 .68rem/1 var(--font-mono);color:var(--danger)}.state[data-active="true"]{color:var(--accent-strong)}.actions{grid-column:1/-1;display:flex;justify-content:flex-end;gap:.45rem}.callout{margin:0;padding:.75rem 1rem;border:1px solid currentColor;border-radius:var(--radius-sm);background:var(--surface-raised)}.success{color:var(--accent-strong)}.dialog-backdrop{position:fixed;inset:0;z-index:60;display:grid;place-items:center;padding:1rem;background:rgb(0 0 0/.48)}.confirm{width:min(100%,470px);padding:1.3rem}.confirm footer{justify-content:flex-end;margin-top:1.2rem}@media(max-width:720px){.admin-head{align-items:flex-start}.overall{font-size:.8rem}.admin-tabs{width:100%;overflow:auto}.admin-tabs button{flex:1}.status-rail{grid-template-columns:1fr}.status-rail article{border-right:0;border-bottom:1px solid var(--border);padding:.75rem}.overview-grid,.storage-grid,.form-grid,.users-layout{grid-template-columns:1fr}.overview-grid .build{grid-column:auto}.build dl{grid-template-columns:1fr}.create-user{position:static}.actions{justify-content:flex-start;flex-wrap:wrap}.settings-form footer{align-items:flex-start;flex-direction:column}}
</style>
