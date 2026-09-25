/**
 * Tailwind config for the OlliTeX "kit" (shadcn-flavored UI layer, Option B).
 *
 * The host application remains Bootstrap/react-bootstrap based; Tailwind is
 * scoped deliberately narrow:
 *  - `preflight` is OFF so Tailwind's CSS reset cannot touch existing pages.
 *  - Theme values are HSL channel tokens on CSS variables (--kit-*), defined
 *    per Overleaf theme in frontend/stylesheets/olkit.scss
 *    (body[data-theme='default'] = dark, body[data-theme='light'] = light).
 *  - Kit classes are opt-in: pages that don't use the kit are unaffected
 *    except for the (small) generated utilities file.
 */
module.exports = {
  darkMode: ['class'],
  corePlugins: { preflight: false },
  content: [
    '../../frontend/js/**/*.{ts,tsx,js,jsx}',
    './modules/*/frontend/js/**/*.{ts,tsx,js,jsx}',
    './app/views/**/*.pug',
  ],
  theme: {
    extend: {
      colors: {
        border: 'hsl(var(--kit-border))',
        input: 'hsl(var(--kit-input))',
        ring: 'hsl(var(--kit-ring))',
        background: 'hsl(var(--kit-background))',
        foreground: 'hsl(var(--kit-foreground))',
        primary: {
          DEFAULT: 'hsl(var(--kit-primary))',
          foreground: 'hsl(var(--kit-primary-foreground))',
        },
        secondary: {
          DEFAULT: 'hsl(var(--kit-secondary))',
          foreground: 'hsl(var(--kit-secondary-foreground))',
        },
        muted: {
          DEFAULT: 'hsl(var(--kit-muted))',
          foreground: 'hsl(var(--kit-muted-foreground))',
        },
        accent: {
          DEFAULT: 'hsl(var(--kit-accent))',
          foreground: 'hsl(var(--kit-accent-foreground))',
        },
        destructive: {
          DEFAULT: 'hsl(var(--kit-destructive))',
          foreground: 'hsl(var(--kit-destructive-foreground))',
        },
        card: {
          DEFAULT: 'hsl(var(--kit-card))',
          foreground: 'hsl(var(--kit-card-foreground))',
        },
      },
      borderRadius: {
        lg: 'var(--kit-radius)',
        md: 'calc(var(--kit-radius) - 2px)',
        sm: 'calc(var(--kit-radius) - 4px)',
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'Avenir', 'Helvetica', 'sans-serif'],
      },
      boxShadow: {
        card: '0 1px 2px 0 hsl(var(--kit-shadow) / 0.08), 0 4px 16px -4px hsl(var(--kit-shadow) / 0.12)',
      },
    },
  },
  plugins: [],
}
