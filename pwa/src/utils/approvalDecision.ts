import { ref } from 'vue'

export const APPROVAL_DECISION_TIMEOUT_MS = 10_000

/**
 * Keeps one approval decision in flight until the session snapshot changes.
 * A bounded timeout lets the user retry when the daemon never reports a new
 * snapshot (for example, after a control-channel failure).
 */
export function createApprovalDecisionGuard(timeoutMs = APPROVAL_DECISION_TIMEOUT_MS) {
  const inFlightId = ref('')
  let timeout: ReturnType<typeof setTimeout> | null = null

  function clear(expectedId = '') {
    if (expectedId && inFlightId.value !== expectedId) return
    if (timeout) clearTimeout(timeout)
    timeout = null
    inFlightId.value = ''
  }

  function begin(approvalId: string): boolean {
    if (!approvalId || inFlightId.value === approvalId) return false
    clear()
    inFlightId.value = approvalId
    timeout = setTimeout(() => clear(approvalId), timeoutMs)
    return true
  }

  function sync(currentApprovalId?: string) {
    if (inFlightId.value && currentApprovalId !== inFlightId.value) clear()
  }

  function isPending(approvalId?: string): boolean {
    return !!approvalId && inFlightId.value === approvalId
  }

  return {
    inFlightId,
    begin,
    clear,
    sync,
    isPending,
    dispose: clear
  }
}
