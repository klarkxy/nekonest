import { createRouter, createWebHistory } from 'vue-router'
import { getPhoneSecret } from '../api/http'
import { appRoutes } from './routes'
import { routePageTitle, setDocumentTitle } from './title'

const router = createRouter({
  history: createWebHistory(),
  routes: appRoutes,
  scrollBehavior: () => ({ top: 0 })
})

router.beforeEach((to) => {
  if (to.meta.public) return true
  // Soft gate: if never set secret, go to setup once.
  // Empty secret is allowed only when server also has no secret (dev).
  if (!getPhoneSecret() && to.name !== 'setup') {
    // Allow through if user already dismissed — we use a flag
    const skipped = localStorage.getItem('nekonest_setup_done')
    if (!skipped) {
      return { name: 'setup' }
    }
  }
  return true
})

router.afterEach((to) => {
  if (to.name === 'session-detail' || to.name === 'device-detail') {
    // Views set a richer title after data loads.
    setDocumentTitle(routePageTitle(to.name))
    return
  }
  setDocumentTitle(routePageTitle(to.name))
})

export { routePageTitle, setDocumentTitle }
export default router
