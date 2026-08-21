/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./static/streamInfo.html', './static/streamInfo-*.js'],
  theme: {
    extend: {
      colors: {
        hh: {
          bg: '#0a0f14', bg2: '#0d141b', panel: '#101922', panel2: '#131f2a',
          line: '#1e2d3a', line2: '#2a3f52', ink: '#e5edf4', muted: '#8ba1b5',
          faint: '#5d7488', cyan: '#38d9e8', 'cyan-dim': '#1a8a97',
          green: '#4ade80', amber: '#fbbf24', red: '#f87171', blue: '#60a5fa', violet: '#a78bfa'
        }
      },
      fontFamily: {
        sans: ['Inter', 'ui-sans-serif', 'system-ui', '-apple-system', '"Segoe UI"', 'sans-serif'],
        mono: ['"IBM Plex Mono"', 'ui-monospace', '"SFMono-Regular"', 'Consolas', 'monospace']
      },
      maxWidth: { screen: '1480px' },
      borderRadius: { hh: '8px' },
      keyframes: {
        blink: { '50%': { opacity: '.4' } },
        spin: { to: { transform: 'rotate(360deg)' } },
        slide: { '0%': { transform: 'translateX(-100%)' }, '100%': { transform: 'translateX(350%)' } }
      },
      animation: { blink: 'blink 1.6s infinite', spin: 'spin .8s linear infinite', slide: 'slide 1s ease-in-out infinite' }
    }
  },
  plugins: []
};
