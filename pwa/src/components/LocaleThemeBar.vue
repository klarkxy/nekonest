<template>
  <div class="prefs-bar" role="group" :aria-label="t('locale.label') + ' / ' + t('theme.label')">
    <div class="prefs-group">
      <span class="prefs-label">{{ t('locale.label') }}</span>
      <button
        type="button"
        class="prefs-chip"
        :class="{ active: locale === 'zh-CN' }"
        :aria-pressed="locale === 'zh-CN'"
        @click="onLocale('zh-CN')"
      >{{ t('locale.zh') }}</button>
      <button
        type="button"
        class="prefs-chip"
        :class="{ active: locale === 'en' }"
        :aria-pressed="locale === 'en'"
        @click="onLocale('en')"
      >{{ t('locale.en') }}</button>
    </div>
    <div class="prefs-group">
      <span class="prefs-label">{{ t('theme.label') }}</span>
      <button
        v-for="opt in themeOptions"
        :key="opt"
        type="button"
        class="prefs-chip"
        :class="{ active: themePref === opt }"
        :aria-pressed="themePref === opt"
        @click="onTheme(opt)"
      >{{ t(`theme.${opt}`) }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { setLocale, type AppLocale } from '@/i18n'
import {
  applyTheme,
  getThemePreference,
  setThemePreference,
  type ThemePreference
} from '@/i18n/theme'

const emit = defineEmits<{ localeChange: [] }>()
const { t, locale } = useI18n()
const themePref = ref<ThemePreference>(getThemePreference())
const themeOptions: ThemePreference[] = ['system', 'light', 'dark']

function onLocale(next: AppLocale) {
  setLocale(next)
  emit('localeChange')
}

function onTheme(next: ThemePreference) {
  themePref.value = next
  setThemePreference(next)
}

watch(locale, () => emit('localeChange'))
applyTheme(themePref.value)
</script>

<style scoped>
.prefs-bar {
  display: grid;
  gap: 8px;
  margin-top: 16px;
  padding: 10px 12px;
  border: 1px solid var(--neko-line);
  border-radius: 14px;
  background: var(--neko-surface-muted);
}

.prefs-group {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.prefs-label {
  margin-right: 4px;
  color: var(--neko-ink-soft);
  font-size: 12px;
  font-weight: 650;
}

.prefs-chip {
  min-height: 44px;
  padding: 0 12px;
  border: 1px solid transparent;
  border-radius: 11px;
  color: var(--neko-ink-soft);
  background: transparent;
  font-size: 12px;
  font-weight: 650;
  cursor: pointer;
}

.prefs-chip.active {
  color: var(--neko-primary-deep);
  border-color: rgba(114, 91, 157, 0.28);
  background: var(--neko-primary-soft);
}

.prefs-chip:hover {
  background: rgba(114, 91, 157, 0.1);
}
</style>
