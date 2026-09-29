import type { Config } from 'tailwindcss';

const config: Config = {
  content: ['./src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        ink: '#0f172a',
        accent: '#4f46e5',
      },
    },
  },
  plugins: [],
};

export default config;
