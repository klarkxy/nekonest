<template>
  <main class="handoff-error">
    <img class="logo" src="/brand/nekonest-duo.webp" :alt="t('brand.duoAlt')" width="88" height="88" />
    <p class="eyebrow">{{ t('handoff.eyebrow') }}</p>
    <h1>{{ t('handoff.title') }}</h1>
    <p class="message">{{ failure?.message || t('handoff.fallback') }}</p>
    <a v-if="failure?.action_url" class="action" :href="failure.action_url">{{ t('handoff.returnCloud') }}</a>
    <router-link v-else class="action" to="/setup">{{ t('handoff.openSetup') }}</router-link>
    <LocaleThemeBar />
  </main>
</template>

<script setup lang="ts">
import { onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import LocaleThemeBar from '@/components/LocaleThemeBar.vue'
import { clearHandoffFailure, readHandoffFailure } from '@/api/handoff'

const { t } = useI18n()
const failure = readHandoffFailure()
onUnmounted(clearHandoffFailure)
</script>

<style scoped>
.handoff-error {
  max-width: 480px;
  margin: 0 auto;
  padding: 56px 24px calc(32px + var(--neko-safe-bottom, 0px));
  text-align: center;
}
.logo { border-radius: 26px; object-fit: cover; box-shadow: 0 14px 30px rgba(104, 74, 105, .18); }
.eyebrow { margin: 22px 0 8px; color: var(--neko-ink-soft); font-size: 12px; letter-spacing: .12em; text-transform: uppercase; }
h1 { margin: 0; color: var(--neko-ink); font-family: var(--neko-display); font-size: 28px; }
.message { margin: 16px auto 24px; color: var(--neko-ink-soft); line-height: 1.65; }
.action { display: inline-flex; min-height: 44px; align-items: center; padding: 0 20px; border-radius: 999px; color: white; background: var(--neko-accent); text-decoration: none; font-weight: 700; }
</style>
