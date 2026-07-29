type ServiceWorkerContainerLike = {
  readonly controller: ServiceWorker | null
  addEventListener(type: 'controllerchange', listener: EventListener): void
  removeEventListener(type: 'controllerchange', listener: EventListener): void
  register(
    scriptURL: string,
    options?: RegistrationOptions
  ): Promise<ServiceWorkerRegistration>
}

/**
 * Register the application worker and refresh when an updated worker takes
 * control. The first controller acquired after a fresh install is ignored so
 * installing the PWA cannot create a reload loop.
 */
export async function registerAppServiceWorker(
  serviceWorker: ServiceWorkerContainerLike,
  reload: () => void = () => window.location.reload()
): Promise<ServiceWorkerRegistration> {
  let hasController = Boolean(serviceWorker.controller)
  let reloaded = false

  const onControllerChange: EventListener = () => {
    if (!hasController) {
      hasController = true
      return
    }
    if (reloaded) return
    reloaded = true
    reload()
  }

  serviceWorker.addEventListener('controllerchange', onControllerChange)
  try {
    return await serviceWorker.register('/sw.js', { updateViaCache: 'none' })
  } catch (error) {
    serviceWorker.removeEventListener('controllerchange', onControllerChange)
    throw error
  }
}
