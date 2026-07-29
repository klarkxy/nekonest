import { createRouter, createWebHistory } from 'vue-router'
import { getPhoneSecret } from '../api/http'
import { appRoutes } from './routes'

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
  const pageTitle =
    to.name === 'devices'
      ? '猫娘窝'
      : to.name === 'device-detail'
        ? '工作目录'
        : to.name === 'session-detail'
          ? '线团'
          : to.name === 'pair'
            ? '配对电脑'
            : to.name === 'setup'
              ? '手机钥匙'
              : ''
  document.title = pageTitle ? `${pageTitle} · 猫娘窝` : '猫娘窝'
})

export default router
