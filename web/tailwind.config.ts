import type { Config } from 'tailwindcss';

/**
 * NOTE: This project runs Tailwind v4 via `@tailwindcss/postcss` and does NOT
 * reference this file with an `@config` directive, so Tailwind does not load
 * it — the real source of truth for tokens (colors, fonts, severity scale) is
 * the `@theme` block in `src/styles/globals.css`.
 *
 * This file is kept only so editor tooling / IntelliSense has a config to
 * point at, and is mirrored against globals.css to avoid drift. If you ever
 * wire it back in with `@config "../tailwind.config.ts";`, keep these values
 * in sync with the `@theme` block.
 */
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // Mirrors the desaturated v0.4 severity scale in globals.css @theme.
        severity: {
          critical: 'oklch(0.55 0.18 25)',
          high:     'oklch(0.65 0.16 50)',
          medium:   'oklch(0.72 0.14 80)',
          low:      'oklch(0.62 0.15 230)',
          info:     'oklch(0.70 0.04 260)',
        },
      },
      fontFamily: {
        sans: ['Plus Jakarta Sans', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif'],
        mono: ['JetBrains Mono', 'SF Mono', 'Menlo', 'Consolas', 'monospace'],
      },
    },
  },
} satisfies Config;
