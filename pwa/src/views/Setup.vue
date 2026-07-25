<template>
  <div class="setup-page">
    <div class="logo">🐱</div>
    <h1>NekoNest</h1>
    <p class="desc">连接你的猫娘窝前，请输入与 VPS 上相同的手机访问密钥。</p>

    <n-input
      v-model:value="secret"
      type="password"
      show-password-on="click"
      placeholder="NEKONEST_PHONE_SECRET"
      size="large"
      class="secret-input"
    />

    <n-button type="primary" block size="large" :disabled="!secret.trim()" @click="save">
      进入猫娘窝
    </n-button>

    <p class="hint">
      在 VPS 上设置环境变量 <code>NEKONEST_PHONE_SECRET</code>。
      若未设置密钥，可随便填一个字符后进入（仅限内网调试）。
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NInput } from 'naive-ui'
import { setPhoneSecret, getPhoneSecret } from '@/api/http'

const router = useRouter()
const secret = ref(getPhoneSecret())

function save() {
  setPhoneSecret(secret.value.trim() || 'dev')
  localStorage.setItem('nekonest_setup_done', '1')
  router.replace('/')
}
</script>

<style scoped>
.setup-page {
  padding: 48px 24px;
  text-align: center;
}
.logo {
  font-size: 64px;
  margin-bottom: 8px;
}
h1 {
  margin: 0 0 12px;
  font-size: 28px;
}
.desc {
  color: #6a6a6a;
  font-size: 14px;
  line-height: 1.6;
  margin-bottom: 24px;
}
.secret-input {
  margin-bottom: 16px;
  text-align: left;
}
.hint {
  margin-top: 24px;
  font-size: 12px;
  color: #999;
  line-height: 1.5;
}
code {
  background: #f0eeeb;
  padding: 1px 6px;
  border-radius: 4px;
}
</style>
