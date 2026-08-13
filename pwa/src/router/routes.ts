import type { RouteRecordRaw } from 'vue-router'
import DeviceList from '../views/DeviceList.vue'
import DeviceDetail from '../views/DeviceDetail.vue'
import SessionDetail from '../views/SessionDetail.vue'
import PairDevice from '../views/PairDevice.vue'
import Setup from '../views/Setup.vue'
import HandoffError from '../views/HandoffError.vue'
import { deviceDetailLocation } from './navigation'

export const appRoutes: RouteRecordRaw[] = [
  {
    path: '/setup',
    name: 'setup',
    component: Setup,
    meta: { public: true }
  },
  {
    path: '/handoff-error',
    name: 'handoff-error',
    component: HandoffError,
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
    component: SessionDetail
  },
  {
    path: '/pair',
    name: 'pair',
    component: PairDevice
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/'
  }
]
