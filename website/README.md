# Tales website

Source for [tales.dev](https://hyperxlab.github.io/tales/) — landing page (`/`) and documentation (`/docs/`).

Built with [Astro](https://astro.build) 6, [Starlight](https://starlight.astro.build) and [Tailwind CSS](https://tailwindcss.com) 4. Output is fully static and deploys to GitHub Pages (workflow lives in `.github/workflows/deploy-website.yml`).

## Local development

Requires [Bun](https://bun.sh) and Node 22.

```sh
bun install
bun run dev       # http://localhost:4321/tales/
bun run build     # production build to ./dist/
bun run preview   # serve ./dist/ locally
```

The landing page is `src/pages/index.astro` (custom Astro + Tailwind). Documentation lives under `src/content/docs/docs/**/*.mdx` and is served by Starlight.

## Project layout

```
website/
├── astro.config.mjs           # Astro + Starlight + Tailwind v4 config
├── src/
│   ├── pages/index.astro      # marketing landing page
│   ├── components/
│   │   ├── landing/           # landing-page sections
│   │   └── starlight/         # Starlight component overrides (Head, etc.)
│   ├── content/docs/docs/     # MDX documentation (nested under docs/ for /docs/ URL)
│   ├── content.config.ts      # Starlight content collection
│   ├── styles/
│   │   ├── global.css         # Tailwind v4 entry + design tokens
│   │   └── starlight.css      # Starlight CSS variable overrides
│   ├── assets/                # logo SVGs, MDX-imported images
│   └── i18n/                  # i18n strings for the landing page
└── public/                    # favicon, OG image, static assets
```

## i18n

The site is English-only at launch but the routing is configured for future translations. The default locale (`en`) is unprefixed: `/docs/...`. Translations will be added under `src/content/docs/<lang>/docs/...` and routed under `/<lang>/docs/...` (e.g. `/fr/docs/...`).

## Deployment

Production build is published by `.github/workflows/deploy-website.yml` on every push to `master` that touches `website/**`. Override `SITE_URL` and `BASE_PATH` to retarget the build (for example for a custom domain set `BASE_PATH=/`).

## Search

Search is provided by [Pagefind](https://pagefind.app), generated at build time. No external service required.
