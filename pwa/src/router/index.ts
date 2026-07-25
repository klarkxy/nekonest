import { createRouter, createWebHistory } from 'vue-router'
import DeviceList from '../views/DeviceList.vue'
import DeviceDetail from '../views/DeviceDetail.vue'
import SessionList from '../views/SessionList.vue'
import NewSession from '../views/NewSession.vue'
import SessionDetail from '../views/SessionDetail.vue'
import PairDevice from '../views/PairDevice.vue'
import Setup from '../views/Setup.vue'
import { getPhoneSecret } from '../api/http'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/setup',
      name: 'setup',
      component: Setup,
      meta: { public: true }
    },
    {
      path: '/',
      name: 'devices',
      component: DeviceList
    },
    {
      path: '/device/:deviceId',
      name: 'device-detail',
      component: DeviceDetail
    },
    {
      path: '/device/:deviceId/sessions',
      name: 'sessions',
      component: SessionList
    },
    {
      path: '/device/:deviceId/new-session',
      name: 'new-session',
      component: NewSession
    },
    {
      path: '/device/:deviceId/session/:sessionId',
      name: 'session-detail',
      component: SessionDetail
    },
    {
      path: '/pair',
      name: 'pair',
      component: PairDevice
    }
  ]
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

export default router
