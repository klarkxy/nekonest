<template>
  <div class="pair-page">
    <div class="page-header">
      <n-button text @click="$router.back()">← 返回</n-button>
      <h1>配对电脑</h1>
    </div>

    <div class="pair-content">
      <div class="pair-icon">🐱</div>
      <p class="pair-desc">
        在 Windows 上执行 <code>nekonest-daemon -register</code> 或
        <code>-pair gen</code> 后会打印 6 位配对码，输入后即可在手机上看到该电脑。
      </p>

      <n-input
        v-model:value="pairCode"
        placeholder="输入 6 位配对码"
        size="large"
        maxlength="6"
        class="pair-input"
      />

      <n-button
        type="primary"
        block
        size="large"
        :loading="pairing"
        @click="handlePair"
        :disabled="pairCode.length !== 6"
      >
        配对！
      </n-button>

      <div class="pair-help">
        <p class="help-title">💡 步骤</p>
        <ol class="help-steps">
          <li>VPS 已部署 Server，并设置 <code>NEKONEST_PHONE_SECRET</code></li>
          <li>PC：<code>set NEKONEST_SERVER=https://你的域名</code></li>
          <li>PC：<code>nekonest-daemon -register -name "书房"</code></li>
          <li>把打印的 6 位码填到上方</li>
          <li>PC：再运行 <code>nekonest-daemon</code> 保持在线</li>
        </ol>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NInput, useMessage } from 'naive-ui'
import { apiFetch } from '@/api/http'
import { useBindingStore } from '@/stores/binding'

const router = useRouter()
const message = useMessage()
const binding = useBindingStore()
const pairCode = ref('')
const pairing = ref(false)

async function handlePair() {
  pairing.value = true
  try {
    const res = await apiFetch('/api/pair/consume', {
      method: 'POST',
      body: JSON.stringify({ code: pairCode.value.trim() })
    })

    if (res.status === 401) {
      throw new Error('密钥无效，请回到设置页检查 NEKONEST_PHONE_SECRET')
    }
    if (!res.ok) {
      throw new Error('配对码无效或已过期')
    }

    const data = await res.json()
    binding.addBinding(data.device_id, data.name || data.device_id)
    message.success('配对成功！🎉')
    router.push('/')
  } catch (err: unknown) {
    const m = err instanceof Error ? err.message : '配对失败'
    message.error(m)
  } finally {
    pairing.value = false
  }
}
</script>

<style scoped>
.pair-page {
  padding: 20px;
}
.pair-content {
  padding: 20px 0;
}
.pair-icon {
  font-size: 64px;
  text-align: center;
  margin-bottom: 20px;
}
.pair-desc {
  text-align: center;
  color: #6a6a6a;
  font-size: 14px;
  line-height: 1.6;
  margin-bottom: 24px;
}
.pair-input {
  margin-bottom: 16px;
}
.pair-input :deep(.n-input-wrapper) {
  text-align: center;
  font-size: 24px;
  font-family: monospace;
  letter-spacing: 8px;
}
.pair-help {
  margin-top: 32px;
  padding: 16px;
  background: #f5f3f0;
  border-radius: 12px;
}
.help-title {
  font-weight: 600;
  margin: 0 0 8px;
}
.help-steps {
  margin: 0;
  padding-left: 20px;
  font-size: 13px;
  color: #555;
  line-height: 1.7;
}
code {
  background: #e8e4df;
  padding: 1px 4px;
  border-radius: 3px;
  font-size: 12px;
}
</style>
