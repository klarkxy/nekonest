<template>
  <n-config-provider :theme="null" :theme-overrides="nekoThemeOverrides">
    <n-message-provider>
      <div class="nekonest-app">
        <a class="skip-link" href="#main-content">跳到主内容</a>
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
import { NConfigProvider, NMessageProvider } from 'naive-ui'
import { nekoThemeOverrides } from './styles/theme'
</script>
