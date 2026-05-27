# Tales website

Source for [tales-testing.github.io/tales](https://tales-testing.github.io/tales/), landing page (`/`) and documentation (`/docs/`).

Built with [Astro](https://astro.build) 6, [Starlight](https://starlight.astro.build) and [Tailwind CSS](https://tailwindcss.com) 4. Output is fully static and deploys to Cloudflare Pages directly from the repository (no GitHub Actions workflow involved).

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

## i18n — adding a new translation

The site is English-only at launch but the routing is configured for future translations. The default locale (`en`) is unprefixed: `/docs/...`. Other languages are routed under `/<lang>/docs/...` (e.g. `/fr/docs/...`).

To add a translation (French in the example below):

1. **Register the locale** in `astro.config.mjs`, inside the `starlight({ locales: { ... } })` block:

   ```js
   locales: {
     root: { label: 'English', lang: 'en' },
     fr:   { label: 'Français', lang: 'fr' },
   }
   ```

2. **Mirror the docs tree** under `src/content/docs/fr/docs/`. Every `.mdx` file in `src/content/docs/docs/` should have a translated counterpart under the `fr/docs/` subtree at the same relative path. Pages without a translation automatically fall back to the English version, so partial translations are safe.

3. **Translate the landing strings** by copying `src/i18n/en.ts` to `src/i18n/fr.ts` and translating the values. The landing page consumes the dictionary statically; switching imports per locale is the next-step refactor (currently every visitor sees the English landing — the Starlight language switcher only affects `/docs/`).

4. **Translate the sidebar labels** by either localising the `label:` fields in the sidebar config, or replacing the static array with Starlight's per-locale sidebar API. The first option is simplest for small translations.

Once a second locale is registered, Starlight's language switcher appears automatically in the header. With only `root`, no switcher is rendered.

## Deployment

The website is deployed by Cloudflare Pages from this repository. The build command is `bun run build` and the output directory is `website/dist`. Override `SITE_URL` and `BASE_PATH` in the Cloudflare Pages project settings to retarget the build (for a root-domain deploy, set `BASE_PATH=/`).

## Search

Search is provided by [Pagefind](https://pagefind.app), generated at build time. No external service required.
