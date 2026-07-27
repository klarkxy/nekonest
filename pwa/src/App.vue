<template>
  <n-config-provider :theme="null" :theme-overrides="nekoThemeOverrides">
    <n-message-provider>
      <div class="nekonest-app">
        <a class="skip-link" href="#main-content">跳到主内容</a>
        <div class="neko-bg" aria-hidden="true">
          <div class="paw p1">🐾</div>
          <div class="paw p2">🐾</div>
          <div class="paw p3">🐾</div>
        </div>
        <main id="main-content" class="main-content">
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

<style>
body {
  margin: 0;
  padding: 0;
  background-color: #F6F0F8;
  font-family: 'Noto Sans SC', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  -webkit-font-smoothing: antialiased;
}

.nekonest-app {
  position: relative;
  max-width: 480px;
  margin: 0 auto;
  min-height: 100vh;
  min-height: 100dvh;
  padding-top: env(safe-area-inset-top, 0);
  padding-bottom: env(safe-area-inset-bottom, 0);
  background:
    linear-gradient(180deg, rgba(255, 240, 248, 0.9) 0%, rgba(250, 248, 255, 0.95) 40%, #FAF8F5 100%);
  overflow-x: hidden;
}
.skip-link {
  position: absolute;
  left: -9999px;
  top: 0;
  z-index: 100;
  padding: 8px 12px;
  background: #6B5BB8;
  color: #fff;
  border-radius: 0 0 8px 0;
  font-size: 14px;
}
.skip-link:focus {
  left: 0;
}
.main-content {
  position: relative;
  z-index: 1;
  min-height: inherit;
}

.neko-bg {
  pointer-events: none;
  position: fixed;
  inset: 0;
  max-width: 480px;
  margin: 0 auto;
  z-index: 0;
  overflow: hidden;
}
.nekonest-app > :not(.neko-bg) {
  position: relative;
  z-index: 1;
}
.neko-bg .paw {
  position: absolute;
  opacity: 0.07;
  font-size: 42px;
  filter: grayscale(0.2);
}
.neko-bg .p1 { top: 12%; left: 8%; transform: rotate(-18deg); }
.neko-bg .p2 { top: 48%; right: 6%; transform: rotate(22deg); font-size: 56px; }
.neko-bg .p3 { bottom: 10%; left: 18%; transform: rotate(-8deg); font-size: 36px; }
</style>
