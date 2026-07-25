<template>
  <n-config-provider :theme="null" :theme-overrides="nekoThemeOverrides">
    <n-message-provider>
      <div class="nekonest-app">
        <router-view />
      </div>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { NConfigProvider, NMessageProvider } from 'naive-ui'
import { nekoThemeOverrides } from './styles/theme'
</script>

<style>
body {
  margin: 0;
  padding: 0;
  background-color: #FAF8F5;
  font-family: 'Noto Sans SC', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  -webkit-font-smoothing: antialiased;
}

.nekonest-app {
  max-width: 480px;
  margin: 0 auto;
  min-height: 100vh;
  background-color: #FAF8F5;
}
</style>
