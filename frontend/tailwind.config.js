/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx,svelte}",
  ],
  theme: {
    extend: {
      colors: {
        'app-bg': '#0d0d1a',
        'app-surface': '#1a1a2e',
        'app-border': 'rgba(255, 255, 255, 0.1)',
      }
    },
  },
  plugins: [],
}
