// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import mdx from '@astrojs/mdx';
import sitemap from '@astrojs/sitemap';
import tailwindcss from '@tailwindcss/vite';
import { visit } from 'unist-util-visit';

const SITE = process.env.SITE_URL ?? 'https://taleslabs.org';
const BASE = process.env.BASE_PATH ?? '/';

// Prepend BASE to absolute Markdown links so /docs/foo/ deploys correctly
// under a sub-path like /tales/. External, mailto, hash, and protocol-relative
// links are left alone.
//
// When BASE is "/" (custom-domain deploy), the function is a no-op: there is
// nothing to prefix and every absolute link already resolves correctly. We
// still return a valid `() => (tree) => {}` plugin so remark accepts it.
//
// Note: when trailingSlash ever changes from 'always', revisit the
// `url !== base` skip: today a bare `/tales` cannot appear in our content.
function remarkAbsolutePathBasePrefix() {
	const base = BASE.replace(/\/$/, '');
	if (!base) {
		return () => (_tree) => undefined;
	}

	return () => (tree) => {
		visit(tree, 'link', (node) => {
			const url = node.url;
			if (typeof url !== 'string' || url.length === 0) return;
			if (
				url.startsWith('/') &&
				!url.startsWith('//') &&
				!url.startsWith(`${base}/`) &&
				url !== base
			) {
				node.url = `${base}${url}`;
			}
		});
	};
}

export default defineConfig({
	site: SITE,
	base: BASE,
	trailingSlash: 'always',
	integrations: [
		starlight({
			title: 'Tales',
			description:
				'End-to-end tests, written once, replayable forever, in a single Go binary.',
			logo: {
				light: './src/assets/logo-light.svg',
				dark: './src/assets/logo-dark.svg',
				replacesTitle: true,
			},
			favicon: '/favicon.svg',
			// global.css is imported by LandingLayout for the marketing pages.
			// Loading it inside Starlight breaks docs pages: its unlayered body
			// rule pins the background to the dark Tailwind token even when
			// Starlight switches into light theme, leaving headings (colored
			// via `--sl-color-white`, dark in light mode) unreadable.
			customCss: ['./src/styles/starlight.css'],
			social: [
				{
					icon: 'github',
					label: 'GitHub',
					href: 'https://github.com/tales-testing/tales',
				},
			],
			editLink: {
				baseUrl: 'https://github.com/tales-testing/tales/edit/master/website/',
			},
			defaultLocale: 'root',
			locales: {
				root: { label: 'English', lang: 'en' },
			},
			pagefind: true,
			lastUpdated: true,
			components: {
				Head: './src/components/starlight/Head.astro',
			},
			sidebar: [
				{
					label: 'Get started',
					items: [
						{ label: 'Introduction', slug: 'docs/getting-started/introduction' },
						{ label: 'Installation', slug: 'docs/getting-started/installation' },
						{ label: 'Your first scenario', slug: 'docs/getting-started/first-scenario' },
						{ label: 'CLI essentials', slug: 'docs/getting-started/cli-essentials' },
					],
				},
				{
					label: 'Writing scenarios',
					items: [
						{ label: 'DSL overview', slug: 'docs/writing-scenarios/dsl-overview' },
						{ label: 'Step-local vars', slug: 'docs/writing-scenarios/step-vars' },
						{ label: 'Captures & result chaining', slug: 'docs/writing-scenarios/captures' },
						{ label: 'Conditional execution', slug: 'docs/writing-scenarios/conditional-execution' },
						{ label: 'Retry & teardown', slug: 'docs/writing-scenarios/retry-teardown' },
						{ label: 'Keywords', slug: 'docs/writing-scenarios/keywords' },
					],
				},
				{
					label: 'Providers',
					items: [
						{ label: 'HTTP', slug: 'docs/providers/http' },
						{ label: 'SQL (Postgres / MySQL)', slug: 'docs/providers/sql' },
						{ label: 'Mobile iOS', slug: 'docs/providers/mobile-ios' },
						{ label: 'Browser (Chrome / Chromium)', slug: 'docs/providers/browser' },
						{ label: 'Load (HTTP benchmarks)', slug: 'docs/providers/load' },
						{ label: 'Keyword', slug: 'docs/providers/keyword' },
					],
				},
				{
					label: 'Generators & expressions',
					items: [
						{ label: 'Generators', slug: 'docs/reference/generators' },
						{ label: 'Built-in functions', slug: 'docs/reference/functions' },
						{ label: 'Matchers', slug: 'docs/reference/matchers' },
						{ label: 'Expression variables', slug: 'docs/reference/variables' },
					],
				},
				{
					label: 'Reports & CI',
					items: [
						{ label: 'Console & exit codes', slug: 'docs/reports/console' },
						{ label: 'JUnit XML', slug: 'docs/reports/junit' },
						{ label: 'JSONL stream', slug: 'docs/reports/jsonl' },
						{ label: 'Visual HTML report', slug: 'docs/reports/visual' },
						{ label: 'CI integration', slug: 'docs/reports/ci-integration' },
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Signing webhooks', slug: 'docs/guides/signing-webhooks' },
						{ label: 'Deterministic test data', slug: 'docs/guides/determinism' },
						{ label: 'Polling with retry', slug: 'docs/guides/polling' },
						{ label: 'Multi-environment', slug: 'docs/guides/multi-environment' },
						{ label: 'Debugging failures', slug: 'docs/guides/debugging-failures' },
					],
				},
				{
					label: 'Operations',
					items: [
						{ label: 'tales doctor', slug: 'docs/operations/doctor' },
						{ label: 'iOS driver cache', slug: 'docs/operations/ios-driver-cache' },
						{ label: 'Release & verification', slug: 'docs/operations/release' },
					],
				},
			],
		}),
		mdx(),
		sitemap(),
	],
	markdown: {
		remarkPlugins: [remarkAbsolutePathBasePrefix()],
	},
	vite: {
		plugins: [tailwindcss()],
	},
});
