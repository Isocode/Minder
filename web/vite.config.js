import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Vite configuration for the Minder web front‑end.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'https://localhost:8443',
        changeOrigin: true,
        secure: false
      }
    }
  }
});