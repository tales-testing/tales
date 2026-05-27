// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import mdx from '@astrojs/mdx';
import sitemap from '@astrojs/sitemap';
import tailwindcss from '@tailwindcss/vite';

const SITE = process.env.SITE_URL ?? 'https://hyperxlab.github.io';
const BASE = process.env.BASE_PATH ?? '/tales';

export default defineConfig({
	site: SITE,
	base: BASE,
	trailingSlash: 'always',
	integrations: [
		starlight({
			title: 'Tales',
			description:
				'End-to-end tests, written once, replayable forever — in a single Go binary.',
			logo: {
				light: './src/assets/logo-light.svg',
				dark: './src/assets/logo-dark.svg',
				replacesTitle: true,
			},
			favicon: '/favicon.svg',
			customCss: ['./src/styles/global.css', './src/styles/starlight.css'],
			social: [
				{
					icon: 'github',
					label: 'GitHub',
					href: 'https://github.com/hyperxlab/tales',
				},
			],
			editLink: {
				baseUrl: 'https://github.com/hyperxlab/tales/edit/master/website/',
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
	vite: {
		plugins: [tailwindcss()],
	},
});
