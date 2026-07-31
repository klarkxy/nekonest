<template>
  <div class="pair-page">
    <div class="page-header">
      <RouterLink class="back-link" :to="devicesLocation()" :aria-label="t('common.backNest')">{{ t('common.back') }}</RouterLink>
      <h1>{{ t('pair.title') }}</h1>
    </div>

    <div class="pair-content">
      <div class="pair-icon">
        <img
          src="/brand/nekonest-duo.webp"
          :alt="t('brand.duoAlt')"
          width="96"
          height="96"
        />
      </div>
      <p class="pair-desc">
        {{ t('pair.desc') }}
      </p>

      <form class="pair-form" @submit.prevent="handlePair">
        <label class="field-label" for="pair-code">{{ t('pair.codeLabel') }}</label>
        <n-input
          v-model:value="pairCodeModel"
          name="one-time-code"
          autocomplete="one-time-code"
          inputmode="text"
          autocapitalize="none"
          spellcheck="false"
          :placeholder="t('pair.placeholder')"
          size="large"
          class="pair-input"
          aria-describedby="pair-help"
          :status="fieldError ? 'error' : undefined"
          :input-props="{ id: 'pair-code' }"
        />

        <label class="field-label" for="pair-qr">{{ t('pair.qrLabel') }}</label>
        <n-input
          v-model:value="qrPaste"
          type="textarea"
          :placeholder="t('pair.qrPlaceholder')"
          :rows="3"
          class="pair-input"
          :input-props="{ id: 'pair-qr' }"
          @update:value="onQrPaste"
        />

        <p v-if="expectedFingerprint" class="fingerprint" role="status">
          {{ t('pair.fingerprint') }}:
          <code translate="no">{{ expectedFingerprint }}</code>
        </p>

        <p v-if="fieldError" class="field-error" role="alert">{{ fieldError }}</p>

        <n-button
          type="primary"
          attr-type="submit"
          block
          size="large"
          :loading="pairing"
          :disabled="pairCode.length !== 6"
        >
          {{ t('pair.submit') }}
        </n-button>
      </form>

      <div id="pair-help" class="pair-help">
        <p class="help-title">{{ t('pair.helpTitle') }}</p>
        <ol class="help-steps">
          <li>
            <span>{{ t('pair.help1') }}</span>
            <div class="command-row">
              <code translate="no">{{ WINDOWS_PAIR_COMMANDS.pair }}</code>
              <button
                type="button"
                :aria-label="t('pair.copyCmd', { cmd: WINDOWS_PAIR_COMMANDS.pair })"
                @click="copyCommand(WINDOWS_PAIR_COMMANDS.pair)"
              >{{ t('common.copy') }}</button>
            </div>
          </li>
          <li>
            <span>{{ t('pair.help2') }}</span>
            <div class="command-row">
              <code translate="no">{{ WINDOWS_PAIR_COMMANDS.register }}</code>
              <button
                type="button"
                :aria-label="t('pair.copyCmd', { cmd: WINDOWS_PAIR_COMMANDS.register })"
                @click="copyCommand(WINDOWS_PAIR_COMMANDS.register)"
              >{{ t('common.copy') }}</button>
            </div>
          </li>
          <li>{{ t('pair.help3') }}</li>
          <li>{{ t('pair.help4') }}</li>
        </ol>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter, RouterLink } from 'vue-router'
import { NButton, NInput, useMessage } from 'naive-ui'
import { apiFetch, setPhoneId, setPhoneToken } from '@/api/http'
import {
  loadOrCreatePhoneIdentity,
  parsePairQRPayload,
  shortFingerprint,
  type PairQRPayload
} from '@/crypto/identity'
import { completePairKeySetup } from '@/crypto/keys'
import { useBindingStore } from '@/stores/binding'
import { devicesLocation } from '@/router/navigation'
import { normalizePairCode, WINDOWS_PAIR_COMMANDS } from '@/utils/onboarding'

const { t } = useI18n()
const router = useRouter()
const message = useMessage()
const binding = useBindingStore()
const pairCode = ref('')
const qrPaste = ref('')
const expectedFingerprint = ref('')
const trustedQR = ref<PairQRPayload | null>(null)
const pairing = ref(false)
const fieldError = ref('')
const pairCodeModel = computed({
  get: () => pairCode.value,
  set: (value: string) => {
    pairCode.value = normalizePairCode(value)
    fieldError.value = ''
  }
})

function onQrPaste(value: string) {
  qrPaste.value = value
  fieldError.value = ''
  const qr = parsePairQRPayload(value)
  if (!qr) {
    trustedQR.value = null
    expectedFingerprint.value = ''
    return
  }
  trustedQR.value = qr
  if (qr.code) pairCode.value = normalizePairCode(qr.code)
  expectedFingerprint.value = shortFingerprint(qr.identity_fingerprint || '', 32)
}

async function handlePair() {
  fieldError.value = ''
  const code = normalizePairCode(pairCode.value)
  if (code.length !== 6 || pairing.value) return
  pairCode.value = code
  pairing.value = true
  try {
    // Ensure phone E2E identity exists (IndexedDB).
    const phoneId = await loadOrCreatePhoneIdentity()

    const res = await apiFetch('/api/pair/consume', {
      method: 'POST',
      body: JSON.stringify({
        code,
        expected_fingerprint: trustedQR.value?.identity_fingerprint || undefined,
        device_id: trustedQR.value?.device_id || undefined,
        phone_ed25519_public: phoneId.ed25519_public,
        phone_x25519_public: phoneId.x25519_public
      })
    })

    if (res.status === 401) {
      fieldError.value = t('pair.errAuth')
      return
    }
    if (res.status === 409) {
      fieldError.value = t('pair.errFingerprint')
      return
    }
    if (!res.ok) {
      fieldError.value = t('pair.errCode')
      return
    }

    const data = (await res.json()) as {
      device_id: string
      name?: string
      phone_id?: string
      phone_token?: string
      identity_fingerprint?: string
      ed25519_public?: string
      x25519_public?: string
    }

    if (
      trustedQR.value?.identity_fingerprint &&
      data.identity_fingerprint &&
      trustedQR.value.identity_fingerprint !== data.identity_fingerprint
    ) {
      fieldError.value = t('pair.errFingerprint')
      return
    }
    if (
      trustedQR.value?.device_id &&
      data.device_id &&
      trustedQR.value.device_id !== data.device_id
    ) {
      fieldError.value = t('pair.errFingerprint')
      return
    }

    if (data.phone_token) setPhoneToken(data.phone_token)
    if (data.phone_id) setPhoneId(data.phone_id)
    binding.addBinding(data.device_id, data.name || data.device_id)

    // Derive wrap key and pull catalog key packages when daemon keys are known.
    const daemonEd = trustedQR.value?.ed25519_public || data.ed25519_public || ''
    const daemonX = trustedQR.value?.x25519_public || data.x25519_public || ''
    if (daemonEd && daemonX) {
      try {
        await completePairKeySetup({
          code,
          deviceId: data.device_id,
          daemonEd25519: daemonEd,
          daemonX25519: daemonX,
          qr: trustedQR.value
        })
      } catch {
        /* open mode still works without packages */
      }
    }

    message.success(t('pair.success'))
    void router.push(devicesLocation())
  } catch {
    fieldError.value = t('pair.errNetwork')
  } finally {
    pairing.value = false
  }
}

async function copyCommand(command: string) {
  try {
    if (!navigator.clipboard) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(command)
    message.success(t('pair.copied'))
  } catch {
    message.error(t('pair.copyFail'))
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
  margin-bottom: 12px;
}

.fingerprint {
  margin: 0 0 12px;
  font-size: 12px;
  color: var(--neko-ink-soft);
  word-break: break-all;
}

.fingerprint code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  color: var(--neko-ink);
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
  background: var(--neko-surface);
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
  background: var(--neko-panel);
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
