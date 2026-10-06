import { defineConfig } from 'vite';
import preact from '@preact/preset-vite';

export default defineConfig({
  plugins: [preact()],
  build: {
    outDir: 'dist',
    // npm run build recreates dist/.gitkeep so the Go embed directive always compiles.
    emptyOutDir: true,
    // Inline data: URLs would need a looser CSP; emit every asset as a file instead.
    assetsInlineLimit: 0,
    modulePreload: { polyfill: false },
    sourcemap: false,
  },
  test: {
    environment: 'jsdom',
  },
});
