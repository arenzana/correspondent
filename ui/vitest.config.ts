import { defineConfig, mergeConfig } from 'vitest/config'
import viteConfig from './vite.config.ts'

export default mergeConfig(
  viteConfig,
  defineConfig({
    // Svelte compiles runes in .svelte.ts for the browser; resolve the browser build.
    resolve: { conditions: ['browser'] },
    define: { __BUILD_ID__: JSON.stringify('test') },
    test: {
      environment: 'happy-dom',
      include: ['src/**/*.test.ts'],
      restoreMocks: true,
    },
  }),
)
