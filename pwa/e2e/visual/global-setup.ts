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
        port: 5173,
        strictPort: true
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
