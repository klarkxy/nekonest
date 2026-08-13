/// <reference types="vite/client" />

declare const __NEKONEST_APP_VERSION__: string

interface ImportMetaEnv {
  readonly VITE_NEKONEST_API_BASE?: string
  readonly VITE_NEKONEST_WS_BASE?: string
  readonly VITE_NEKONEST_MANAGED?: string
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}
