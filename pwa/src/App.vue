<template>
  <n-config-provider
    :theme="naiveTheme"
    :theme-overrides="themeOverrides"
  >
    <n-message-provider>
      <div class="nekonest-app">
        <a class="skip-link" href="#main-content">{{ t('brand.skipToContent') }}</a>
        <div class="neko-atmosphere" aria-hidden="true">
          <span class="ambient-orb ambient-orb--rose"></span>
          <span class="ambient-orb ambient-orb--violet"></span>
          <span class="ambient-ribbon"></span>
          <span class="ambient-spark ambient-spark--one">✦</span>
          <span class="ambient-spark ambient-spark--two">✧</span>
          <span class="ambient-spark ambient-spark--three">✦</span>
        </div>
        <main id="main-content" class="main-content" tabindex="-1">
          <router-view />
        </main>
      </div>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { darkTheme, NConfigProvider, NMessageProvider } from 'naive-ui'
import {
  nekoThemeOverrides,
  nekoThemeOverridesDark
} from './styles/theme'
import {
  applyTheme,
  getThemePreference,
  resolveTheme,
  watchSystemTheme,
  type ResolvedTheme
} from './i18n/theme'
import { syncVisualViewportHeight } from './utils/visualViewport'

const { t } = useI18n()
const resolved = ref<ResolvedTheme>(resolveTheme(getThemePreference()))

const naiveTheme = computed(() => (resolved.value === 'dark' ? darkTheme : null))
const themeOverrides = computed(() =>
  resolved.value === 'dark' ? nekoThemeOverridesDark : nekoThemeOverrides
)

let stopWatch: (() => void) | undefined
let stopViewportSync: (() => void) | undefined

onMounted(() => {
  resolved.value = applyTheme()
  stopViewportSync = syncVisualViewportHeight()
  stopWatch = watchSystemTheme((next) => {
    resolved.value = next
  })
  window.addEventListener('nekonest-theme', onThemeEvent as EventListener)
})

onUnmounted(() => {
  stopWatch?.()
  stopViewportSync?.()
  window.removeEventListener('nekonest-theme', onThemeEvent as EventListener)
})

function onThemeEvent() {
  resolved.value = resolveTheme(getThemePreference())
}
</script>
