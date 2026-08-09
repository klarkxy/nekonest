import { createServer } from 'vite'
import { startVisualMock } from './mock-server.mjs'

export default async function globalSetup() {
  process.env.NEKONEST_DEV_API = 'http://127.0.0.1:18080'
  const stopMock = await startVisualMock()
  try {
    const vite = await createServer({
      configFile: 'vite.config.ts',
      server: {
        host: '127.0.0.1',
        // Windows has reserved the default Vite 5173 range on this host.
        port: 4173,
        strictPort: true,
        proxy: {
          '/health': {
            target: process.env.NEKONEST_DEV_API
          }
        }
      }
    })
    await vite.listen()
    return async () => {
      await vite.close()
      await stopMock()
    }
  } catch (error) {
    await stopMock()
    throw error
  }
}
