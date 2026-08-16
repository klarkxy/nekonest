import type { RouteRecordRaw } from 'vue-router'
import DeviceList from '../views/DeviceList.vue'
import { deviceDetailLocation } from './navigation'

export const appRoutes: RouteRecordRaw[] = [
  {
    path: '/setup',
    name: 'setup',
    component: () => import('../views/Setup.vue'),
    meta: { public: true }
  },
  {
    path: '/handoff-error',
    name: 'handoff-error',
    component: () => import('../views/HandoffError.vue'),
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
    component: () => import('../views/DeviceDetail.vue')
  },
  {
    path: '/device/:deviceId/sessions',
    name: 'sessions',
    redirect: to => deviceDetailLocation(String(to.params.deviceId || ''))
  },
  {
    path: '/device/:deviceId/new-session',
    name: 'new-session',
    redirect: to => deviceDetailLocation(String(to.params.deviceId || ''))
  },
  {
    path: '/device/:deviceId/session/:sessionId',
    name: 'session-detail',
    component: () => import('../views/SessionDetail.vue')
  },
  {
    path: '/pair',
    name: 'pair',
    component: () => import('../views/PairDevice.vue')
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/'
  }
]
