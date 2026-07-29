import { describe, expect, it, vi } from 'vitest'
import { registerAppServiceWorker } from './serviceWorker'

class FakeServiceWorkerContainer extends EventTarget {
  controller: ServiceWorker | null
  register = vi.fn().mockResolvedValue({ scope: '/' } as ServiceWorkerRegistration)

  constructor(hasController: boolean) {
    super()
    this.controller = hasController ? ({} as ServiceWorker) : null
  }
}

describe('registerAppServiceWorker', () => {
  it('reloads once when an update replaces an existing controller', async () => {
    const serviceWorker = new FakeServiceWorkerContainer(true)
    const reload = vi.fn()

    await registerAppServiceWorker(serviceWorker, reload)
    serviceWorker.dispatchEvent(new Event('controllerchange'))
    serviceWorker.dispatchEvent(new Event('controllerchange'))

    expect(serviceWorker.register).toHaveBeenCalledWith(
      '/sw.js',
      { updateViaCache: 'none' }
    )
    expect(reload).toHaveBeenCalledOnce()
  })

  it('does not reload when a fresh install first takes control', async () => {
    const serviceWorker = new FakeServiceWorkerContainer(false)
    const reload = vi.fn()

    await registerAppServiceWorker(serviceWorker, reload)
    serviceWorker.dispatchEvent(new Event('controllerchange'))

    expect(reload).not.toHaveBeenCalled()

    serviceWorker.dispatchEvent(new Event('controllerchange'))
    expect(reload).toHaveBeenCalledOnce()
  })
})
