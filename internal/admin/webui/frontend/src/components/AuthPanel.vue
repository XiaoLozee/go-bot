<script setup lang="ts">
import { computed, ref, watch } from 'vue'

const props = defineProps<{
  mode: 'login' | 'setup'
  busy: boolean
  error: string
}>()

const emit = defineEmits<{
  submit: [password: string]
}>()

const password = ref('')
const confirmPassword = ref('')
const localError = ref('')

watch(
  () => props.mode,
  () => {
    password.value = ''
    confirmPassword.value = ''
    localError.value = ''
  },
)

const submitLabel = computed(() => (props.mode === 'setup' ? '初始化并登录' : '登录'))
const currentError = computed(() => localError.value || props.error)

function handleSubmit() {
  localError.value = ''
  if (!password.value.trim()) {
    localError.value = '密码不能为空。'
    return
  }
  if (props.mode === 'setup' && password.value !== confirmPassword.value) {
    localError.value = '两次输入的密码不一致。'
    return
  }
  emit('submit', password.value)
}
</script>

<template>
  <section class="auth-shell">
    <div class="auth-stage">
      <div class="auth-hero">
        <span class="auth-brand-badge">Go-bot</span>
        <h1>后台登录</h1>
      </div>

      <div class="auth-form-panel">
        <div v-if="currentError" class="banner banner-danger">
          <strong>错误提示</strong>
          <span>{{ currentError }}</span>
        </div>

        <form class="auth-form" @submit.prevent="handleSubmit">
          <label class="field">
            <span>管理密码</span>
            <input v-model="password" type="password" autocomplete="current-password" :disabled="busy" placeholder="输入管理密码" />
          </label>

          <label v-if="mode === 'setup'" class="field">
            <span>确认密码</span>
            <input v-model="confirmPassword" type="password" autocomplete="new-password" :disabled="busy" placeholder="再次输入密码" />
          </label>

          <button class="primary-btn auth-submit-btn" type="submit" :disabled="busy">
            {{ busy ? '提交中...' : submitLabel }}
          </button>
        </form>
      </div>
    </div>
  </section>
</template>

<style scoped>
.auth-shell {
  min-height: calc(100vh - 48px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px 0;
}

.auth-stage {
  position: relative;
  overflow: hidden;
  width: min(560px, 100%);
  margin: 0 auto;
  border-radius: 28px;
  border: 1px solid rgba(255, 255, 255, 0.18);
  background: linear-gradient(145deg, rgba(255, 255, 255, 0.16), rgba(255, 255, 255, 0.08));
  backdrop-filter: blur(22px);
  -webkit-backdrop-filter: blur(22px);
  box-shadow: var(--shell-shadow);
}

.auth-stage::before {
  content: '';
  position: absolute;
  inset: 1px;
  border-radius: 26px;
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.16), transparent 28%, transparent 72%, rgba(255, 255, 255, 0.08));
  pointer-events: none;
}

.auth-stage > * {
  position: relative;
  z-index: 1;
}

.auth-hero {
  position: relative;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 22px 24px 18px;
  background: var(--hero-gradient);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.12);
  color: #ffffff;
}

.auth-hero::before,
.auth-hero::after {
  content: '';
  position: absolute;
  border-radius: 999px;
  pointer-events: none;
}

.auth-hero::before {
  width: 180px;
  height: 180px;
  top: -54px;
  right: -36px;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.18) 0%, rgba(255, 255, 255, 0) 72%);
}

.auth-hero::after {
  width: 220px;
  height: 220px;
  left: -72px;
  bottom: -132px;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.2) 0%, rgba(255, 255, 255, 0) 70%);
}

.auth-brand-badge {
  display: inline-flex;
  align-items: center;
  width: fit-content;
  min-height: 34px;
  padding: 0 14px;
  border-radius: 999px;
  border: 1px solid var(--hero-pill-border);
  background: var(--hero-pill-bg);
  color: var(--hero-pill-text);
  font-size: 12px;
  font-weight: 700;
}

.auth-brand-badge {
  letter-spacing: 0.04em;
}

.auth-hero h1,
.auth-form-panel p {
  margin: 0;
}

.auth-hero h1 {
  font-size: 28px;
  line-height: 1.08;
}

.auth-form-panel {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 24px;
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.92), var(--surface-soft-alt));
}

.auth-form-panel::before {
  content: '';
  position: absolute;
  inset: 0;
  background: radial-gradient(circle at top right, var(--accent-ring) 0%, transparent 34%);
  pointer-events: none;
}

.auth-form-panel > * {
  position: relative;
  z-index: 1;
}

.auth-submit-btn {
  width: 100%;
  min-height: 46px;
}

@media (max-width: 980px) {
  .auth-shell {
    min-height: auto;
    padding: 16px 0;
  }

  .auth-stage {
    border-radius: 24px;
  }

  .auth-hero,
  .auth-form-panel {
    padding: 20px;
  }

  .auth-hero h1 {
    font-size: 26px;
  }
}
</style>
