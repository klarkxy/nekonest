import type { RouteLocationRaw } from 'vue-router'

export function devicesLocation(): RouteLocationRaw {
  return { name: 'devices' }
}

export function setupLocation(): RouteLocationRaw {
  return { name: 'setup' }
}

export function pairLocation(): RouteLocationRaw {
  return { name: 'pair' }
}

export function deviceDetailLocation(deviceId: string): RouteLocationRaw {
  return {
    name: 'device-detail',
    params: { deviceId }
  }
}

export function sessionDetailLocation(
  deviceId: string,
  sessionId: string
): RouteLocationRaw {
  return {
    name: 'session-detail',
    params: { deviceId, sessionId }
  }
}
