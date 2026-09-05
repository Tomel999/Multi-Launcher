import {defineConfig} from 'vite'
import preact from '@preact/preset-vite'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [preact()],
  server: {
    proxy: {
      // plugin frontends are served by the Go asset server, not vite
      '/plugins/': 'http://localhost:34115',
    },
  },
})
