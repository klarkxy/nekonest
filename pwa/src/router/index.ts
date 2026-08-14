import { createRouter, createWebHistory } from 'vue-router'
import { getPhoneSecret, getPhoneToken } from '../api/http'
import { appRoutes } from './routes'
import { routePageTitle, setDocumentTitle } from './title'

const router = createRouter({
  history: createWebHistory(),
  routes: appRoutes,
  scrollBehavior: () => ({ top: 0 })
})

export function privateRouteNeedsCredential(isPublic: boolean): boolean {
  if (isPublic) return false
  return !getPhoneToken() && !getPhoneSecret()
}

router.beforeEach((to) => {
  if (to.meta.public) return true
  if (privateRouteNeedsCredential(false) && to.name !== 'setup') {
    return { name: 'setup' }
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
