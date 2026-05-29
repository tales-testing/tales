// English strings for the landing page.
// To translate Tales later, copy this file to `src/i18n/<lang>.ts` and update
// the strings. Components import via `import strings from '../../i18n/en'`.

export default {
	meta: {
		title: 'Tales: declarative end-to-end testing in a single Go binary',
		description:
			'End-to-end tests, written once, replayable forever, in a single Go binary. An open-source alternative to Robot Framework, Karate, and Venom, with HCL2 syntax, deterministic faker, HTTP + SQL + Browser + iOS + Load providers (Android soon), and a visual HTML report.',
	},

	nav: {
		docs: 'Docs',
		github: 'GitHub',
		getStarted: 'Get started',
	},

	hero: {
		eyebrow: 'Integration testing, reinvented',
		headline:
			'End-to-end tests, written once, replayable forever, in a single Go binary.',
		subhead:
			'Tales is the integration testing tool we wished existed. A modern alternative to Robot Framework, Karate, and Venom, without the Python toolchain to babysit, the JavaScript creep, or the YAML soup. One declarative HCL2 syntax, one seedable run, one tool for API, SQL, Browser, iOS, and HTTP load workflows. Android coming soon.',
		ctaPrimary: 'Get started',
		ctaSecondary: 'View on GitHub',
		agentBadge: 'AI agent-ready: ships with a Claude Code skill that writes your tests',
	},

	terminal: {
		command: 'tales test ./e2e/pass --seed 1234 --parallel 4',
		preflight: 'tales: loaded 12 scenarios from 5 files; timeout=disabled',
		lines: [
			{ kind: 'ok', text: 'PASS  e2e/pass/blog.tales / Create blog post (842ms)' },
			{ kind: 'ok', text: 'PASS  e2e/pass/keyword.tales / Use keyword (231ms)' },
			{ kind: 'ok', text: 'PASS  e2e/pass/sql.tales / PostgreSQL operations (95ms)' },
			{
				kind: 'ok',
				text: 'PASS  e2e/pass/file_upload.tales / Multipart upload (47ms)',
			},
			{
				kind: 'ok',
				text: 'PASS  e2e/pass/signed_webhook.tales / Signed webhook (38ms)',
			},
		],
		summary: '12 passed · 0 failed · 0 skipped · 1.24s',
	},

	why: {
		title: 'Why Tales exists',
		subtitle:
			'Built after years of fighting the same problems with Robot Framework, Karate, and Venom.',
		items: [
			{
				title: 'No Python env to babysit',
				body: 'Robot Framework drags a Python toolchain that breaks on every OS update, every pip upgrade, every CI runner refresh. Tales is one static Go binary you drop into CI and forget about.',
			},
			{
				title: 'No DSL-meets-JavaScript creep',
				body: 'Karate scenarios tend to grow JavaScript blocks until they are a codebase. Tales is fully declarative HCL2 with built-in functions, generators, and matchers: what you write is what you read.',
			},
			{
				title: 'No YAML soup either',
				body: 'Venom and similar YAML-driven runners get hard to read once scenarios chain captures and conditionals. HCL2 keeps the same declarative spirit, but with typed values, comments, and expressions that scale to real workflows.',
			},
			{
				title: 'API, SQL, Browser, iOS, Load in one runner',
				body: 'Stop juggling separate tools for HTTP, database state, browser flows, mobile UI tests, and smoke load. Tales runs them in the same scenario file, with the same syntax, in the same report. Android support is on the roadmap.',
			},
		],
	},

	features: {
		title: 'What you get out of the box',
		subtitle: 'A focused toolset for the test problems that actually slow teams down.',
		items: [
			{
				title: 'Single binary',
				body: 'Drop `tales` into your CI. No runtime, no plugins, no version manager. Static Go binary for Linux and macOS.',
			},
			{
				title: 'Declarative HCL2',
				body: 'Readable scenarios that diff cleanly. The DSL is the test: no callbacks, no glue code, no JavaScript escape hatch.',
			},
			{
				title: 'Deterministic faker',
				body: 'Generate emails, passwords, people, MAC addresses, bytes. Same seed → same data, every run, on every machine.',
			},
			{
				title: 'Seedable replay',
				body: 'Reproduce a flaky CI failure locally with one flag. `--seed 1234` and your laptop replays exactly what the runner saw.',
			},
			{
				title: 'Five providers, one binary',
				body: 'Drive your API, set up database state (Postgres, MySQL), drive Chrome via CDP with Web performance budgets, tap through an iOS simulator, replay HTTP at concurrent load. Android coming soon.',
			},
			{
				title: 'Parallel by default',
				body: 'Scenarios run concurrently with `--parallel`. Steps inside a scenario stay sequential, so chained captures remain deterministic.',
			},
			{
				title: 'Visual HTML report',
				body: 'A self-contained HTML report with timeline, action tiles, and screenshot replay. Open it. Share it. Debug in two clicks.',
			},
			{
				title: 'CI-native outputs',
				body: 'JUnit XML and JSONL out of the box. Exit codes your pipeline already understands. No glue scripts, no reporters to wire up.',
			},
		],
	},

	quickstart: {
		title: 'See it in 30 seconds',
		subtitle:
			'Three tabs: a scenario, the command that runs it, the report your CI gets.',
		tabs: {
			scenario: 'Scenario',
			cli: 'CLI',
			report: 'JSONL',
		},
		scenario: `version = 1

generator "email" "user_email" {
  prefix = "qa-"
  domain = "example.com"
}

scenario "Create blog post" {
  step "http" "create_user" {
    request {
      method = "POST"
      url    = "https://api.example.com/users"
      body {
        json = {
          email    = generate("user_email")
          password = "Sup3rS3cret!"
        }
      }
    }

    expect {
      status = 201
      json = {
        id    = is_string()
        email = request.body.json.email
      }
    }

    capture {
      id    = response.json.id
      email = response.json.email
    }
  }

  step "http" "create_post" {
    request {
      method  = "POST"
      url     = "https://api.example.com/blog/posts"
      headers = { Author = result.create_user.id }
      body {
        json = {
          title = "Hello from Tales"
          body  = "Reproducible test data, every run."
        }
      }
    }

    expect {
      status = 201
    }
  }

  teardown {
    step "http" "delete_user" {
      when = can(result.create_user.id)
      request {
        method = "DELETE"
        url    = "https://api.example.com/users/\${result.create_user.id}"
      }
      expect { status = one_of([200, 204, 404]) }
    }
  }
}`,
		cli: `# Validate scenarios without running them (parse + reference checks)
$ tales validate ./e2e/pass

# Run the suite with a deterministic seed, 4 scenarios in parallel,
# emit JUnit XML for CI and a single-file visual HTML report
$ tales test ./e2e/pass \\
    --seed 1234 \\
    --parallel 4 \\
    --report-junit  ./reports/junit.xml \\
    --report-html   ./reports/visual.html

tales: loaded 12 scenarios from 5 files; timeout=disabled
PASS  e2e/pass/blog.tales        Create blog post  (842ms)
PASS  e2e/pass/keyword.tales     Use keyword       (231ms)
PASS  e2e/pass/sql.tales         PostgreSQL ops    ( 95ms)
…
Summary: 12 passed · 0 failed · 0 skipped · 1.24s
HTML report: ./reports/visual.html`,
		report: `{"event":"suite_start","seed":1234,"parallel":4,"files":5,"scenarios":12}
{"event":"scenario_start","scenario":"Create blog post","tags":[]}
{"event":"step","scenario":"Create blog post","step":"create_user","status":"pass","duration_ms":312}
{"event":"step","scenario":"Create blog post","step":"create_post","status":"pass","duration_ms":418}
{"event":"teardown_step","scenario":"Create blog post","step":"delete_user","status":"pass","duration_ms":112}
{"event":"scenario_end","scenario":"Create blog post","status":"pass","duration_ms":842}
{"event":"suite_end","status":"pass","passed":12,"failed":0,"skipped":0,"duration_ms":1240}`,
	},

	useCases: {
		title: 'Built for real-world test problems',
		subtitle: 'Pick a starting point. Every use case is one binary away.',
		items: [
			{
				tag: 'API',
				title: 'HTTP workflows',
				body: 'Chain requests with captured IDs, assert JSON with matchers, sign webhooks with HMAC, upload multipart files. The HTTP provider is the heart of Tales.',
				snippet: `step "http" "send_signed_webhook" {
  vars {
    ts   = now_unix()
    body = jsonencode({ id = "evt-1", type = "ping" })
    sig  = hmac_sha256_hex(config.webhook_secret, "\${vars.ts}.\${vars.body}")
  }

  request {
    method  = "POST"
    url     = "\${config.base_url}/webhook"
    headers = { X-Signature = "t=\${vars.ts},v1=\${vars.sig}" }
    body { raw = vars.body }
  }
}`,
			},
			{
				tag: 'SQL',
				title: 'Database hooks',
				body: 'Run plain SQL statements (Postgres or MySQL) inside a scenario to flip a flag, seed a row, or read internal state the public API does not expose. Not a migration tool, not a fixture loader: a thin escape hatch alongside your HTTP assertions.',
				snippet: `config {
  sql {
    connections {
      pg = { driver = "postgres", dsn = env("POSTGRES_DSN") }
    }
  }
}

step "sql" "insert_org" {
  connection = "pg"
  exec {
    sql  = "INSERT INTO orgs (id, vip) VALUES ($1, $2)"
    args = ["org_123", true]
  }
  expect { json = { rows_affected = 1 } }
}`,
			},
			{
				tag: 'Mobile',
				title: 'iOS smoke tests (Android soon)',
				body: 'Drive a real iOS simulator with an embedded XCUITest driver: zero Swift code to write, no test target to maintain. Visual report shows every tap. Android coming soon.',
				snippet: `step "mobile" "fill_login" {
  platform = "ios"
  target   = "iphone"
  actions {
    input_text {
      id    = "login.email"
      value = "qa@example.com"
    }
    input_text {
      id     = "login.password"
      value  = "secret"
      secure = true
    }
    tap { id = "login.submit" }
    wait_visible { id = "home.screen" }
  }
  expect {
    visible { id = "home.welcome" }
    text {
      id    = "home.user"
      value = contains("Welcome")
    }
  }
}`,
			},
			{
				tag: 'Browser',
				title: 'Web flows + perf budgets',
				body: 'Drive a real Chrome/Chromium session via the Chrome DevTools Protocol. Click, fill, submit, assert visible elements, URL, and title. Pin Web Performance budgets (FCP, LCP, CLS, load) with the same threshold matchers you use everywhere else.',
				snippet: `step "browser" "dashboard_perf" {
  target = "chrome"
  actions {
    goto {
      url = "\${config.base_url}/web/dashboard"
    }
    wait_visible {
      selector = "[data-testid='dashboard.title']"
    }
  }
  expect {
    web_perf {
      fcp = lt("1800ms")
      lcp = lt("2500ms")
      cls = lt(0.1)
    }
  }
}`,
			},
			{
				tag: 'Load',
				title: 'HTTP smoke benchmarks',
				body: 'Replay one HTTP request concurrently for a duration or a fixed request count and assert latency percentiles, RPS, and error/status ratios. Not a replacement for k6 or Gatling — a regression guard you keep next to your normal scenarios.',
				snippet: `step "load" "health" {
  http {
    method = "GET"
    url    = "\${config.base_url}/healthz"
  }
  run {
    requests    = 500
    concurrency = 10
  }
  expect {
    status_2xx_ratio = gte(0.99)
    p95              = lt("200ms")
    error_ratio      = lte(0.01)
  }
}`,
			},
		],
	},

	agents: {
		eyebrow: 'AI agent-ready',
		title: 'Built for the way you code now: with an agent',
		body: 'Tales is designed to be driven by AI coding agents, not just typed by hand. A declarative HCL2 surface, seedable deterministic runs, and structured failure output give an agent exactly what it needs to write a test, run it, read the result, and fix it on its own.',
		bullets: [
			{
				title: 'A dedicated Claude Code skill',
				body: 'Tales ships the `tales-test-generator` skill: it grounds the agent in the DSL source of truth, then generates valid, runnable `.tales` suites (scenarios, keywords, captures, teardown) instead of plausible-looking guesses.',
			},
			{
				title: 'Deterministic, so agents self-correct',
				body: 'Seeded faker plus JSONL output mean the agent gets the same data and the same diagnostics every run. It can reproduce a failure, reason about it, and verify its own fix.',
			},
			{
				title: 'Declarative surface, small blast radius',
				body: 'No glue code, no JavaScript escape hatch. The agent edits one contained HCL block, and what it writes is what runs: easier to generate, easier to review.',
			},
		],
		install: {
			label: 'Install the skill',
			code: 'make install-skill',
			note: 'Copies it to ~/.claude/skills/tales-test-generator.',
		},
		card: {
			skill: 'tales-test-generator',
			prompt: 'Write a Tales suite for the login + refresh-token flow.',
			generated: 'generated .tales',
			snippet: `scenario "Login then refresh" {
  step "http" "login" {
    request {
      method = "POST"
      url    = "\${config.base_url}/auth/login"
      body { json = { email = config.user, password = config.pass } }
    }
    expect { status = 200 }
    capture { token = response.json.access_token }
  }

  step "http" "refresh" {
    request {
      method  = "POST"
      url     = "\${config.base_url}/auth/refresh"
      headers = { Authorization = "Bearer \${result.login.token}" }
    }
    expect { status = 200 }
  }
}`,
		},
	},

	determinism: {
		title: 'Same seed. Same data. Every run.',
		body: 'Tales generators are seeded: pass `--seed 1234` once and your CI gets the same emails, passwords, person names, and IDs as your laptop. No more "works on my machine". No more rerunning a CI job five times hoping the flake goes away.',
		bullet1: 'A single `--seed` flag controls every faker call across every scenario.',
		bullet2: 'Generator outputs are mixed with scenario, step, and generator names, so identical runs produce identical values even under `--parallel`.',
		bullet3: 'Reproduce a red CI build by copying its seed into your local command line. The data lines up byte for byte.',
	},

	install: {
		title: 'Install Tales',
		subtitle: 'Four ways in. Pick whichever fits your stack.',
		homebrew: {
			title: 'Homebrew (macOS / Linux)',
			body: 'The fastest path on a laptop. Linux and macOS, amd64 and arm64.',
			code: 'brew install --cask tales-testing/tap/tales',
		},
		fromRelease: {
			title: 'Pre-built binary',
			body: 'Grab the latest release tarball for Linux or macOS (amd64 / arm64) from GitHub Releases.',
			href: 'https://github.com/tales-testing/tales/releases',
			cta: 'Open releases',
		},
		fromSource: {
			title: 'Build from source',
			body: 'You will need Go 1.26+. The Makefile handles the rest.',
			code: 'git clone https://github.com/tales-testing/tales\ncd tales\nmake install',
		},
		githubAction: {
			title: 'GitHub Action',
			body: 'Drop one step into your workflow to pin and install Tales on the runner. Used by the example CI recipes.',
			code: '- uses: tales-testing/setup-tales-action@v1\n  with:\n    version: latest',
			href: 'https://github.com/tales-testing/setup-tales-action',
			cta: 'View on GitHub',
		},
	},

	footer: {
		tagline:
			'Tales is open source and MIT licensed. Built by developers tired of integration testing being the worst part of their day.',
		columns: [
			{
				title: 'Documentation',
				links: [
					{ label: 'Get started', href: '/docs/getting-started/introduction/' },
					{ label: 'CLI reference', href: '/docs/getting-started/cli-essentials/' },
					{ label: 'Providers', href: '/docs/providers/http/' },
					{ label: 'Generators', href: '/docs/reference/generators/' },
				],
			},
			{
				title: 'Project',
				links: [
					{ label: 'GitHub', href: 'https://github.com/tales-testing/tales' },
					{ label: 'Releases', href: 'https://github.com/tales-testing/tales/releases' },
					{ label: 'Issues', href: 'https://github.com/tales-testing/tales/issues' },
					{ label: 'License (MIT)', href: 'https://github.com/tales-testing/tales/blob/master/LICENSE.md' },
				],
			},
		],
		colophon: 'Built with Astro + Starlight + Tailwind CSS. Hosted on Cloudflare Pages.',
	},
} as const;
