import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    sourcemap: false,
    minify: 'terser',
    rollupOptions: {
      onwarn(warning, warn) {
        // Suppress circular dependency warnings from Material-UI
        if (warning.code === 'CIRCULAR_DEPENDENCY') {
          return
        }
        // Use default for everything else
        warn(warning)
      },
    },
  },
  server: {
    port: 3000,
  },
})
