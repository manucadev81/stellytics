# examples

Reference integrations, also used in the end-to-end tests. Each example is a workspace package of its own (`examples/<name>`).

| Folder | What it shows |
|---|---|
| [`static-html/`](static-html/) | Static site: each loaded page is a page view |
| [`spa/`](spa/) | SPA with no framework, using `history.pushState`: each route change is a page view, without reloading |
| [`nextjs-ssr/`](nextjs-ssr/) | Next.js 16 (App Router) with SSR: the tag comes out in the server HTML, and navigation through `<Link>` counts as a page view |
| [`api-backend/`](api-backend/) | HTTP backend with OpenTelemetry and a dual-export guide (SDK and OTel Collector) for the stellytics collector (#35) |
| [`e2e/`](e2e/) | Playwright: opens the three in Chromium and checks the page views in the Umami database (#27) |

## Script installation (snippets for the onboarding guide)

The script lives on the gateway, with a fixed version in the path and an SRI hash. Replace `<gateway>` with the gateway address and `<website-id>` with the site ID shown in the portal. The hashes of each version are in [`packages/tracker/published/sri.json`](../packages/tracker/published/sri.json).

### Static HTML and SPA

In the `<head>` of every page (in an SPA, only in `index.html`):

```html
<script
  defer
  src="https://<gateway>/s/v1.4.0/n.js"
  integrity="sha384-pm9M1OmFXMUjvFlufTHAabxGja01QLtafXQ54zVP1FsVnSP83VQUELoQCjktI0fr"
  crossorigin="anonymous"
  data-site="<website-id>"
></script>
```

In an SPA nothing else is needed: the script counts the route changes made through `history.pushState`, `history.replaceState` and the browser's back button. Routers such as React Router, Vue Router and SvelteKit's use these APIs.

### Next.js (App Router)

In `app/layout.tsx`, inside the `<head>`:

```tsx
<head>
  <script
    defer
    src="https://<gateway>/s/v1.4.0/n.js"
    integrity="sha384-pm9M1OmFXMUjvFlufTHAabxGja01QLtafXQ54zVP1FsVnSP83VQUELoQCjktI0fr"
    crossOrigin="anonymous"
    data-site="<website-id>"
  />
</head>
```

The tag goes in the HTML the server generates, and navigation through `<Link>` counts as a page view. Do not use `next/script` with `strategy="lazyOnload"`: it loads the script late, and the first page view may be lost.

### Requirements

- **HTTPS:** the JS execution proof uses `crypto.subtle`, which the browser only offers over HTTPS or on `localhost`. On a regular HTTP page, the script does not send events.
- **No cookie and no consent:** the script writes no cookie or storage, and the IP is truncated at the gateway.
- **To change version,** change the path and the `integrity` in the HTML itself. The script never updates itself.

## Running locally

With Compose up (gateway on port 3002) and a site registered in the portal (or created by hand in Umami and in `ingest_site`):

```bash
cd examples/static-html && STELLYTICS_WEBSITE_ID=<id> PORT=4001 node ../serve.mjs .
cd examples/spa && STELLYTICS_WEBSITE_ID=<id> PORT=4002 node ../serve.mjs . spa
cd examples/nextjs-ssr && pnpm build && STELLYTICS_WEBSITE_ID=<id> pnpm start
```

`serve.mjs` replaces the `<!-- stellytics:script -->` marker of the pages with the script tag. The site comes from `STELLYTICS_WEBSITE_ID`, the gateway from `STELLYTICS_GATEWAY_URL` (default `http://127.0.0.1:3002`) and the version from `STELLYTICS_SCRIPT_VERSION` (default: the newest published). The Next.js example reads the same variables.

## End to end

```bash
pnpm --filter @stellytics/examples-e2e exec playwright install chromium
pnpm --filter @stellytics/examples-e2e e2e
```

`run.mjs` creates a website in Umami for each example and a tenant with the three sites in the `stellytics` database. Then it runs Playwright, which starts the examples on ports 4001 to 4003, and at the end it deletes everything.

The tests check:
- **Static HTML:** one page view per page;
- **SPA:** four page views, including the browser's back button, without reloading the page;
- **Next.js:** the tag with `integrity` in the server HTML and navigation through `<Link>`.

The URLs can be changed through `UMAMI_URL`, `STELLYTICS_DATABASE_URL`, `UMAMI_DATABASE_URL` and `STELLYTICS_GATEWAY_URL`. In CI, the test runs in the `compose` job.
