<template>
  <form class="setup-page" @submit.prevent="save">
    <img
      class="logo"
      src="/brand/nekonest-duo.webp"
      :alt="t('brand.duoAlt')"
      width="88"
      height="88"
    />
    <h1>{{ t('setup.title') }}</h1>
    <p class="desc">{{ t('setup.desc') }}</p>

    <label class="field-label" for="phone-secret">{{ t('setup.secretLabel') }}</label>
    <n-input
      v-model:value="secret"
      type="password"
      name="password"
      autocomplete="current-password"
      show-password-on="click"
      :placeholder="t('setup.placeholder')"
      size="large"
      class="secret-input"
      aria-describedby="secret-hint"
      :input-props="{ id: 'phone-secret' }"
    />

    <n-button
      type="primary"
      attr-type="submit"
      block
      size="large"
      :disabled="!secret.trim()"
    >
      {{ t('setup.enter') }}
    </n-button>

    <p id="secret-hint" class="hint">
      {{ t('setup.hint') }}
    </p>

    <LocaleThemeBar />
  </form>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { NButton, NInput } from 'naive-ui'
import { setPhoneSecret, getPhoneSecret } from '@/api/http'
import LocaleThemeBar from '@/components/LocaleThemeBar.vue'
import { devicesLocation } from '@/router/navigation'
import { normalizePhoneSecret } from '@/utils/onboarding'

const { t } = useI18n()
const router = useRouter()
const secret = ref(getPhoneSecret())

function save() {
  const normalized = normalizePhoneSecret(secret.value)
  if (!normalized) return
  setPhoneSecret(normalized)
  localStorage.setItem('nekonest_setup_done', '1')
  void router.replace(devicesLocation())
}
</script>

<style scoped>
.setup-page {
  padding: 48px 24px calc(32px + var(--neko-safe-bottom, 0px));
  text-align: center;
}

.logo {
  width: 88px;
  height: 88px;
  margin: 0 auto 12px;
  border-radius: 26px 26px 32px 18px;
  object-fit: cover;
  box-shadow: 0 14px 30px rgba(104, 74, 105, 0.18);
  border: 3px solid rgba(255, 253, 251, 0.92);
}

h1 {
  margin: 0 0 12px;
  color: var(--neko-ink);
  font-family: var(--neko-display);
  font-size: 28px;
  font-weight: 760;
  letter-spacing: -0.04em;
}

.desc {
  color: var(--neko-ink-soft);
  font-size: 14px;
  line-height: 1.6;
  margin-bottom: 24px;
  text-wrap: pretty;
}

.field-label {
  display: block;
  text-align: left;
  font-size: 13px;
  font-weight: 600;
  color: var(--neko-ink-soft);
  margin-bottom: 8px;
}

.secret-input {
  margin-bottom: 16px;
  text-align: left;
}

.hint {
  margin-top: 24px;
  font-size: 12px;
  color: var(--neko-ink-faint);
  line-height: 1.55;
  text-wrap: pretty;
}
</style>
