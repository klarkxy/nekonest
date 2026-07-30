<template>
  <div class="pair-page">
    <div class="page-header">
      <RouterLink class="back-link" :to="devicesLocation()" aria-label="返回猫窝">← 返回</RouterLink>
      <h1>配对电脑</h1>
    </div>

    <div class="pair-content">
      <div class="pair-icon">
        <img
          src="/brand/nekonest-duo.webp"
          alt="NekoNest 看板娘"
          width="96"
          height="96"
        />
      </div>
      <p class="pair-desc">
        在家里电脑上生成 6 位配对码，填到这里，手机就能看见那台电脑的线团。
      </p>

      <form class="pair-form" @submit.prevent="handlePair">
        <label class="field-label" for="pair-code">配对码</label>
        <n-input
          id="pair-code"
          v-model:value="pairCodeModel"
          name="one-time-code"
          autocomplete="one-time-code"
          inputmode="text"
          autocapitalize="none"
          spellcheck="false"
          placeholder="输入 6 位配对码…"
          size="large"
          class="pair-input"
          aria-describedby="pair-help"
          :status="fieldError ? 'error' : undefined"
        />
        <p v-if="fieldError" class="field-error" role="alert">{{ fieldError }}</p>

        <n-button
          type="primary"
          attr-type="submit"
          block
          size="large"
          :loading="pairing"
          :disabled="pairCode.length !== 6"
        >
          完成配对
        </n-button>
      </form>

      <div id="pair-help" class="pair-help">
        <p class="help-title">怎么拿到配对码</p>
        <ol class="help-steps">
          <li>
            <span>已注册的电脑运行</span>
            <div class="command-row">
              <code translate="no">{{ WINDOWS_PAIR_COMMANDS.pair }}</code>
              <button
                type="button"
                :aria-label="`复制命令 ${WINDOWS_PAIR_COMMANDS.pair}`"
                @click="copyCommand(WINDOWS_PAIR_COMMANDS.pair)"
              >复制</button>
            </div>
          </li>
          <li>
            <span>首次使用时，设置好服务器地址与注册令牌后运行</span>
            <div class="command-row">
              <code translate="no">{{ WINDOWS_PAIR_COMMANDS.register }}</code>
              <button
                type="button"
                :aria-label="`复制命令 ${WINDOWS_PAIR_COMMANDS.register}`"
                @click="copyCommand(WINDOWS_PAIR_COMMANDS.register)"
              >复制</button>
            </div>
          </li>
          <li>把码填到上方，点完成配对</li>
          <li>让本机服务保持在线</li>
        </ol>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { NButton, NInput, useMessage } from 'naive-ui'
import { apiFetch } from '@/api/http'
import { useBindingStore } from '@/stores/binding'
import { devicesLocation } from '@/router/navigation'
import { normalizePairCode, WINDOWS_PAIR_COMMANDS } from '@/utils/onboarding'

const router = useRouter()
const message = useMessage()
const binding = useBindingStore()
const pairCode = ref('')
const pairing = ref(false)
const fieldError = ref('')
const pairCodeModel = computed({
  get: () => pairCode.value,
  set: (value: string) => {
    pairCode.value = normalizePairCode(value)
    fieldError.value = ''
  }
})

async function handlePair() {
  fieldError.value = ''
  const code = normalizePairCode(pairCode.value)
  if (code.length !== 6 || pairing.value) return
  pairCode.value = code
  pairing.value = true
  try {
    const res = await apiFetch('/api/pair/consume', {
      method: 'POST',
      body: JSON.stringify({ code })
    })

    if (res.status === 401) {
      fieldError.value = '手机钥匙不对，请先去钥匙设置。'
      return
    }
    if (!res.ok) {
      fieldError.value = '配对码无效或已过期，请在电脑上重新生成。'
      return
    }

    const data = await res.json()
    binding.addBinding(data.device_id, data.name || data.device_id)
    message.success('配对成功')
    void router.push(devicesLocation())
  } catch {
    fieldError.value = '配对没成功，检查网络后再试。'
  } finally {
    pairing.value = false
  }
}

async function copyCommand(command: string) {
  try {
    if (!navigator.clipboard) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(command)
    message.success('命令已复制')
  } catch {
    message.error('复制失败，请长按命令手动复制')
  }
}
</script>

<style scoped>
.pair-page {
  padding: 20px 20px calc(28px + var(--neko-safe-bottom, 0px));
}

.page-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}

.back-link {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  padding: 4px 2px;
  color: var(--neko-primary-deep);
  font-size: 14px;
  font-weight: 620;
  text-decoration: none;
  white-space: nowrap;
}

.page-header h1 {
  margin: 0;
  color: var(--neko-ink);
  font-family: var(--neko-display);
  font-size: 22px;
  font-weight: 720;
}

.pair-content {
  padding: 12px 0 20px;
}

.pair-icon {
  text-align: center;
  margin-bottom: 18px;
}

.pair-icon img {
  width: 96px;
  height: 96px;
  border-radius: 28px 28px 34px 18px;
  object-fit: cover;
  box-shadow: 0 12px 28px rgba(104, 74, 105, 0.2);
  border: 3px solid rgba(255, 255, 255, 0.9);
  background: linear-gradient(135deg, #F3EEFF, #FFE8F0);
}

.pair-desc {
  text-align: center;
  color: var(--neko-ink-soft);
  font-size: 14px;
  line-height: 1.6;
  margin: 0 0 22px;
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

.pair-input {
  margin-bottom: 8px;
}

.field-error {
  margin: 0 0 12px;
  color: var(--neko-danger);
  font-size: 12px;
  line-height: 1.45;
  text-align: left;
}

.pair-help {
  margin-top: 28px;
  padding: 16px 18px;
  border-radius: 16px;
  background: rgba(255, 252, 250, 0.82);
  border: 1px solid var(--neko-line);
}

.help-title {
  margin: 0 0 10px;
  color: var(--neko-ink);
  font-size: 13px;
  font-weight: 680;
}

.help-steps {
  margin: 0;
  padding-left: 1.2em;
  color: var(--neko-ink-soft);
  font-size: 12px;
  line-height: 1.7;
}

.help-steps li + li {
  margin-top: 8px;
}

.command-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: stretch;
  gap: 8px;
  margin-top: 4px;
}

.command-row code {
  display: flex;
  min-width: 0;
  align-items: center;
  padding: 8px 9px;
  border-radius: 7px;
  color: var(--neko-primary-deep);
  background: rgba(114, 91, 157, 0.08);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
  line-height: 1.5;
  overflow-wrap: anywhere;
  white-space: normal;
}

.command-row button {
  min-width: 52px;
  min-height: 44px;
  padding: 0 10px;
  border: 1px solid rgba(114, 91, 157, 0.2);
  border-radius: 9px;
  color: var(--neko-primary-deep);
  background: rgba(255, 255, 255, 0.72);
  font-size: 12px;
  font-weight: 650;
  cursor: pointer;
}

.command-row button:hover {
  background: rgba(114, 91, 157, 0.1);
}

@media (max-width: 340px) {
  .command-row {
    grid-template-columns: minmax(0, 1fr);
  }

  .command-row button {
    min-width: 68px;
    justify-self: end;
  }
}
</style>
