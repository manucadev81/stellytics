# Tenant onboarding guide

For anyone who wants to measure a project from the Stellar ecosystem on the platform: from sign-up to reading the dashboards, covering **web** analytics (tracking script) and **API** analytics (OpenTelemetry). Written for issue [#43](https://github.com/romulospl/stellytics/issues/43) (pilot criterion pending).

> **Validation status.** The snippets in this guide were checked against the repository examples and against the screens' code (see [PR #108](https://github.com/romulospl/stellytics/pull/108)). Validation with a **real pilot** (acceptance criterion 2 of #43) **has not happened yet**: it depends on the pilots of issues [#45](https://github.com/romulospl/stellytics/issues/45) and [#46](https://github.com/romulospl/stellytics/issues/46). Until then, treat the guide as not validated by an outside project.

Conventions: `<gateway>` is the web ingestion host and `<collector>` is the API collector host (the instance's `api_host`), both as the host name only, without `https://`. Whoever operates the instance gives you both. In production, the URLs are `https://<gateway>` and `https://<collector>`, on port 443; the collector serves OTLP/HTTP and gRPC on the same host ([ADR 0012](adr/0012-ingestao-de-api-pelo-caddy.md)). In local Compose, the gateway is at `http://127.0.0.1:3002` and the collector at `http://127.0.0.1:4318` (HTTP) and `127.0.0.1:4317` (gRPC). No real production has been deployed yet (`docs/andamento.md`); confirm the addresses with whoever operates the instance before configuring.

Terms used in this guide:

- **SCF:** Stellar Community Fund, which funds the projects and is the audience of the stellytics project. An **SCF operator** is an account with a view of all projects.
- **ADR:** a record of an architecture decision of the project, in [`docs/adr/`](adr/README.md).
- **SRI** (Subresource Integrity): the browser checks the hash of the script file before running it and refuses it if it does not match.
- **SPA** (single-page app, which changes screens without reloading) and **SSR** (server-side rendering: the page is built on the server).
- **OpenTelemetry (OTel):** an open standard for metrics, traces and logs. **OTLP** is the protocol it uses to send that data.
- **APM:** the monitoring tool your API already uses. **Dual-export** means sending the same metric to two destinations: your APM and stellytics.
- **Umami:** the web analytics engine used under the hood of the portal.
- **p50 and p95:** the median latency and the value below which 95% of requests fall.
- **Delta and cumulative temporality:** with delta, each send carries only what happened since the previous one; with cumulative, it carries the total since the start.
- **Sampling:** keeping only a share of the traces.

## 1. Overview

A **project** (tenant) groups **sites** (web analytics) and **API services** (API analytics). You only see your own project's data; SCF operators see all projects.

| Step | Where | What comes out of it |
|---|---|---|
| 1. Sign-up and email confirmation | `/sign-up` | confirmed account |
| 2. Create the project | `/onboarding` | project, with you as `owner` |
| 3a. Register a site | `/sites` | site ID for the script |
| 3b. Register an API service | `/services` | ingest key `ntf_...` |
| 4a. Install the script | on your site | page views in the dashboard |
| 4b. Set up OTel dual-export | in your API | metrics in the dashboard |
| 5. Read the dashboards | `/sites/[siteId]`, `/services/[serviceId]` | |

You can use only the web part, only the API part, or both.

Before you start, have at hand:

- the portal address, the `<gateway>` and the `<collector>`, given by whoever operates the instance;
- an email address you can open: sign-up confirmation is required;
- for web analytics: permission to edit the `<head>` of your site and the site served over HTTPS (section 3.4);
- for API analytics: an API that already uses the OpenTelemetry SDK or an OTel Collector, where you can add an exporter (section 4).

## 2. Sign-up

1. Open `/sign-up` and enter name, email and password (minimum of 12 characters, with confirmation).
2. The portal sends an email with the confirmation link, valid for 24 hours. **Without confirming, you cannot sign in or create a project.** With a valid link, you are signed in right away and taken on to the project. If the link expires or is invalid, go to `/sign-in` with email and password: the portal sends a new one (there is an IP rate limit).
3. At `/onboarding`, give the project a name (for example, the name of your product). This creates the project and your `owner` role. If the instance requires prior approval (whoever operates the instance decides this), the project starts as pending: the portal only shows `/dashboard`, as a waiting screen, and unlocks `/sites` and `/services` once an SCF operator approves it. The portal does not send an email about the approval; agree on the timing with whoever operates the instance and come back to the portal to check.
4. `/dashboard` shows the count of sites and services, with links to `/sites` and `/services`.

Forgot the password? `/forgot-password` sends a single-use link, valid for 30 minutes, which ends the other sessions. The portal session lasts 7 days of inactivity (renewed each day of use).

What **does not exist**: inviting other members to the project. A project has the account that created it.

Deleting your own account exists at `/settings`: the portal asks for your email as confirmation and deletes the account and the project's data (sites, services, keys and metrics; details in [`docs/privacidade.md`](privacidade.md)). Whoever only needs to remove a project, without deleting the account, talks to the SCF operators (the removal is done by them, with a recorded reason, and can be reverted).

## 3. Web analytics

### 3.1 Register the site

At `/sites`, enter **Name** and **Domain** (for example `docs.example.org`) and add it. The table now shows the **Website ID**, a UUID. That value goes in the script's `data-site` attribute. The site name leads to its dashboard.

The portal **does not generate the install snippet**: build the tag as below, replacing `<gateway>` and `<website-id>`.

### 3.2 Install the script

The script lives on the gateway, with the version in the path and an SRI hash. The hashes of all versions are in [`packages/tracker/published/sri.json`](../packages/tracker/published/sri.json). The newest version is **v1.4.0**.

**Static HTML and SPA.** In the `<head>` of every page (in an SPA, only in `index.html`):

```html
<script
  defer
  src="https://<gateway>/s/v1.4.0/n.js"
  integrity="sha384-pm9M1OmFXMUjvFlufTHAabxGja01QLtafXQ54zVP1FsVnSP83VQUELoQCjktI0fr"
  crossorigin="anonymous"
  data-site="<website-id>"
></script>
```

In an SPA nothing else is needed: the script counts the route changes made through `history.pushState`, `history.replaceState` and the browser's back button. Routers such as React Router, Vue Router and SvelteKit's use these APIs. Examples: [`examples/static-html`](../examples/static-html), [`examples/spa`](../examples/spa).

**Next.js (App Router, with SSR).** In `app/layout.tsx`, inside the `<head>`:

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

The tag goes in the HTML the server generates, and navigation through `<Link>` counts as a page view. **Do not use `next/script` with `strategy="lazyOnload"`:** it loads the script late, and the first page view may be lost. Example: [`examples/nextjs-ssr`](../examples/nextjs-ssr).

The snippets above are the same as those in [`examples/README.md`](../examples/README.md), which the real examples and the end-to-end test (`examples/e2e`) exercise.

### 3.3 Immutable version and SRI

- Each published version (`/s/vX.Y.Z/n.js`) **never changes**: the file is served with a one-year immutable cache and the hash comes from `sri.json`. If the content changed, the browser would refuse the script because of `integrity`.
- The script **never updates itself**. To change version, change the path and the `integrity` in your HTML.
- `crossorigin="anonymous"` is required together with `integrity`; the gateway answers with `Access-Control-Allow-Origin: *` for that.
- Use version **v1.4.0**, the newest. The table below shows what each published version brings.

| Version | What it brings |
|---|---|
| v1.0.0 | Does not do the JS execution proof, and the gateway **drops its events**. Do not use. |
| v1.1.0 | Page views, with the JS execution proof. |
| v1.2.0 | Custom events, on the old global object `window.notify`. |
| v1.3.0 | Opt-in Wallets Kit adapter (`window.notify.wallets`). |
| v1.4.0 | The global object becomes `window.stellytics` (`event` and `wallets`). |

Versions v1.0.0 to v1.3.0 stay as they were published and do not have `window.stellytics`; the examples in this guide use `window.stellytics` and therefore require v1.4.0.

### 3.4 Requirements and what the script does

- **HTTPS:** the JS execution proof uses `crypto.subtle`, which the browser only offers over HTTPS or on `localhost`. On a regular HTTP page the script does not send events.
- The script asks for a challenge at `GET /c`, solves the proof and sends a page view at `POST /e` to the origin it was loaded from. Events from clients that do not pass the proof are silently dropped (`204`).
- A page view carries: site ID, hostname, path (plus only the `utm_*` parameters), referrer without query or fragment, title, language and screen size.
- **Do not put personal data in paths or titles** (for example `/profile/maria@example.com`): they would be stored.
- Archived site or removed project: the gateway answers as if it had accepted the event and drops it. If the numbers do not show up, check the site ID before anything else.

### 3.4.1 Custom events (v1.2.0 or newer)

Custom events exist since v1.2.0; in versions v1.2.0 and v1.3.0 the global object is `window.notify`. With v1.4.0, which this guide uses, `window.stellytics.event(name, properties)` records an action (a click, a wallet connection, etc.) on the current page:

```js
window.stellytics?.event("signup_clicked", { plan: "pro", seats: 3 });
```

- Name: 1 to 50 characters among letters, digits and `_ . : -`, starting with a letter or digit. Properties: up to 10, with a key of up to 40 characters and a value that is text (up to 100 bytes), a number or true/false. Anything outside this is ignored.
- **Do not send personal or wallet data:** address, public key, transaction hash, transferred amount, email. The gateway only limits type and size; the content is the project's responsibility.
- `window.stellytics` only exists after the script loads, and **there is no queue**: a call before that is lost. Always use `window.stellytics?.event(...)`.
- The web dashboard does not list custom events yet; they stay in Umami.
- **Stellar Wallets Kit events (from v1.3.0; with `window.stellytics`, in v1.4.0; opt-in, not automatic):** the script does **not** capture anything from the kit on its own. It only reaches the kit if **you** hand it over, with one call, after initializing the kit (see [ADR 0010](adr/0010-adaptador-opt-in-do-wallets-kit.md)):

  ```js
  window.stellytics?.wallets(StellarWalletsKit, KitEventType);
  ```

  - Call it **once** (calling it again with the same kit does not duplicate the subscription). It returns a function that cancels the subscription. If the kit or the enum are invalid, nothing happens and the page does not break.
  - It only subscribes to `WALLET_SELECTED` (sends `wallet_selected` with `{ wallet: "freighter" }`, the module ID) and `DISCONNECT` (sends `wallet_disconnected`, with no data). It **never** subscribes to `STATE_UPDATED` and never sends an address, public key, `networkPassphrase`, hash or amount. An ID that is not a plain module name (up to 32 characters) is ignored.
  - Since `window.stellytics` has no queue, call it after the script has loaded (`window.stellytics?.wallets(...)`; if the script may take a while, repeat the call on the window's `load` event, it is idempotent).
  - If you prefer to subscribe by hand and use `stellytics.event`, the handler receives `{ eventType, payload }`: the id is in `e.payload.id`, **never** in `e.id`, and `STATE_UPDATED` stays forbidden (it carries the address):

    ```js
    StellarWalletsKit.on(KitEventType.WALLET_SELECTED, (e) => {
      const id = e?.payload?.id;
      if (id) window.stellytics?.event("wallet_selected", { wallet: id });
    });
    ```

### 3.5 Ad blockers

The script and the endpoints use `/s/`, `/e` and `/c`, without the terms that the default lists block, and a daily check verifies the uBlock Origin lists. Even so, **use a neutral host** for the gateway (for example `e.<your-domain>` or `gw.<your-domain>`), without `analytics`, `track`, `collect`, `umami`, `stats`, `metrics` or `telemetry` in the name. Results, lists tested, limits (AdGuard and DNS filters were not tested) and a manual script in [`packages/tracker/docs/ad-blockers.md`](../packages/tracker/docs/ad-blockers.md). Some share of visitors will still be blocked; the numbers are a measurement, not a census.

## 4. API analytics

### 4.1 Register the service and get the key

1. At `/services`, enter **Service name** (for example `horizon-proxy`) and create it. The portal creates the service **and the first ingest key**.
2. The key shows up **only once**, in a notice "Copy your ingest key now. It won't be shown again". Copy it and store it in a secrets manager. It has the format `ntf_<8 characters>_<secret>`. The portal stores only the hash; if you lose the key, create another.
3. At `/services/[serviceId]/keys` (the **Ingest keys** tab) you see the prefix, the creation date and the status of each key (the **Last used** column shows the day, in UTC, of the key's last authentication on the collector; "Never" means it has not been used yet), create new keys and revoke old ones. Each service can have up to **5 active keys**. To change the key without losing data: create the new one, update your API and only then revoke the old one.

**One key per service.** The key defines the project and the service of each metric. The `service.name` and any tenant attribute your API sends are **ignored**: the collector deletes the `stellytics.*` attributes that come from the client.

### 4.2 What the collector needs to receive

- **Endpoint:** OTLP/HTTP (`/v1/metrics`, `/v1/traces`) or OTLP/gRPC, on the `<collector>`.
- **Header:** `Authorization: Bearer ntf_...`. Without a valid key (or with a revoked key, or one from a removed project), the response is `401` (HTTP) or `Unauthenticated` (gRPC).
- **Signal:** the `http.server.request.duration` histogram (in seconds, the current name in the OpenTelemetry semantic conventions; the old `http.server.duration`, in `ms`, also works). Alternative: HTTP server spans. If the service sends both, stellytics uses the metric and ignores the spans, so as not to count twice. Prefer the metric: sampled traces count fewer requests than the real ones.
- **Attributes read:** `http.request.method`, `http.response.status_code` and `http.route`. Without `http.route`, the collector uses the path (`url.path`) and normalizes it.
- **Temporality:** prefer **delta**. Cumulative also works (the collector converts it), but a collector restart may lose an interval.

**Normalized routes.** An `http.route` you send is used as it came (e.g. `/accounts/{id}`). Without it, the path segments become `{uuid}`, `{address}` (Stellar addresses G, C or M), `{hash}` (hex of 16 characters or more) or `{id}` (digits only), and query, fragment and trailing slash disappear. There is a cap of 500 routes per service per day; anything beyond that goes in as `{other}`. Send `http.route` whenever your framework knows it.

**What the collector drops.** Traces, spans and logs are **never stored**: for each request only route, method, status, count and the duration distribution are kept, aggregated per minute. Even so, do not put personal data in attributes; the safest filter is on your side (a `filter` processor in your Collector).

### 4.3 Dual-export through the SDK

Your API keeps sending telemetry to the current APM and sends the same metric to stellytics. Each destination is a `PeriodicExportingMetricReader` on the same `MeterProvider` (full code in [`examples/api-backend/server.mjs`](../examples/api-backend/server.mjs)):

```js
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-proto";
import { AggregationTemporality, MeterProvider, PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";

const reader = (url, headers = {}) =>
  new PeriodicExportingMetricReader({
    exporter: new OTLPMetricExporter({ url, headers, temporalityPreference: AggregationTemporality.DELTA }),
  });

const provider = new MeterProvider({
  readers: [
    reader("https://apm.example.com/v1/metrics"),
    reader("https://<collector>/v1/metrics", { Authorization: `Bearer ${process.env.STELLYTICS_INGEST_KEY}` }),
  ],
});
```

The first reader sends to your own backend (your APM) and the second to stellytics. In the SDK, the stellytics URL is `https://<collector>/v1/metrics`; in local Compose, `http://127.0.0.1:4318/v1/metrics`.

The standard `OTEL_EXPORTER_OTLP_*` variables configure only one destination; the second has to be built in code (or use the Collector). In other languages the idea is the same: two OTLP exporters, one with the `Authorization` header.

### 4.4 Dual-export through the OTel Collector

If your API already sends to an OTel Collector, just add an exporter (full file in [`examples/api-backend/otelcol.yaml`](../examples/api-backend/otelcol.yaml)). In the Collector, `STELLYTICS_OTLP_ENDPOINT` is `https://<collector>`, with no path (in local Compose, `http://127.0.0.1:4318`): the exporter appends `/v1/metrics` on its own. Snippet:

```yaml
exporters:
  otlp_http/stellytics:
    endpoint: ${env:STELLYTICS_OTLP_ENDPOINT}
    headers:
      Authorization: Bearer ${env:STELLYTICS_INGEST_KEY}

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug, otlp_http/stellytics]
```

In the `exporters` list of the pipeline, replace `debug` with your APM's exporter. It works with the Collector core (`otel/opentelemetry-collector`) from v0.164.0; in older versions, the exporter name is `otlphttp`. With contrib, you can use a separate pipeline with a `filter` processor before `otlp_http/stellytics`, sending only what is needed. More details, including running locally with Compose, in [`examples/api-backend/README.md`](../examples/api-backend/README.md); the full contract is in [`docs/specs/e5-pipeline-de-api.md`](specs/e5-pipeline-de-api.md).

### 4.5 Check that it arrived

The **Last used** column of the keys has one-day granularity and is written in the background on the first authentication of the day, so it may take a few moments. The most direct sign that the data arrived is the service dashboard (section 5.2), which starts showing requests within 1 to 2 minutes, depending on the SDK's export interval. If "No requests in this period yet" appears, check: endpoint, `Authorization` header, the metric name and whether your API really received traffic after you configured the exporter.

## 5. Read the dashboards

Both dashboards have the period selector **Last 24 hours, 7 days, 30 days, 90 days and 12 months**, default 7 days. All times are **UTC**. The period ends now and starts at the beginning of an hour, day or month, so "Last 7 days" is the 6 previous whole days plus today up to now. The period is in the URL (`?period=30d`), so you can share the link with anyone else on the project.

### 5.1 A site's dashboard (`/sites/[siteId]`)

- **Page views, Unique visitors, Average visit duration and Bounce rate**, each with the change against the previous period of the same length.
- A chart of page views per hour, day or month (depending on the period), with an alternative table.
- Lists of **Referrers, Countries, Devices and Browsers**.
- A unique visitor is an estimate made without cookies or a persistent identifier in the browser (section 7 summarizes privacy); the exact criterion belongs to Umami and was not checked here, so do not treat the number as a count of people.
- If Umami is down, the dashboard shows "Web analytics is unavailable right now"; try again in a few minutes.

### 5.2 An API service's dashboard (`/services/[serviceId]`)

- **Requests**, **Server errors (5xx)** (percentage and count), **Median latency (p50)** and **p95 latency**.
- Charts of requests (with 4xx and 5xx in the detail) and of latency over the period, with an alternative table.
- **Endpoints**: method and normalized route, with requests, % of 4xx and 5xx, p50 and p95; it shows the **100 routes** with the most requests (the header says "Top 100 of N").
- **Status codes**: distribution by code.
- The **Ingest keys** tab leads to key management (section 4.1).

How to read the numbers and their limits:

- **p50 and p95 are approximations.** Latency is stored in 15 buckets (14 limits, from 5 ms to 10 s, plus one bucket above 10 s) and the percentile is interpolated within the range; the maximum error is the width of the range. It is good for trends and comparison, not for measuring milliseconds. Latency above 10 s shows as 10 s.
- **Delay:** new data shows up within 1 to 2 minutes, depending on the SDK's export interval. A job rolls the minutes up into hours and days every 15 minutes, only with hours closed for 5 minutes; meanwhile the dashboard reads the recent part straight from the minutes, so the difference should not show up in the numbers.
- **Retention:** minutes for 35 days, hours and days for 25 months. The dashboard reads each part from the table that covers it: the rolled-up hours come from 1h, the days from 1d, and the recent, not yet rolled-up part, from 1m.
- **Gaps** in the latency charts are periods with no requests.
- **Server HTTP only:** the dashboard does not show clients, IPs, bodies or traces; none of that ever reaches stellytics.
- **Removed project:** a project removed by the SCF only sees the notice, and ingestion (web and API) is refused.

## 6. Common problems

| Symptom | Likely cause |
|---|---|
| No page views | wrong `data-site`; page over HTTP (no `crypto.subtle`); script v1.0.0; blocked by an ad blocker; `integrity` does not match the version in the path |
| Browser console refuses the script | `integrity` from another version, or `crossorigin="anonymous"` is missing |
| Next.js first page view vanished | `next/script` with `lazyOnload`; put the tag in the layout's `<head>` |
| `401` when exporting metrics | wrong, revoked or removed-project key; the header must be `Authorization: Bearer ntf_...` |
| API dashboard empty with a valid key | metric or spans that are not HTTP server ones; check `http.request.method` and `http.response.status_code` |
| Many `{other}` routes | `http.route` missing with high-cardinality paths; provide `http.route` |
| I lost the key | it cannot be recovered; create another and revoke the old one |

## 7. Privacy in short

- **Web:** no cookie, no `localStorage` or `sessionStorage`. The gateway **truncates the IP** (last octet in IPv4, /48 in IPv6) before geolocation, and the full IP is not stored. Session recording and heatmaps are off.
- **API:** only counters and histograms per route, method and status; traces, spans and logs are dropped.
- Since it uses no cookie, the script does not require a consent banner **by itself**; the legal decision is your project's. The roles, what is stored and the points of attention are in [`docs/privacidade.md`](privacidade.md), which is not legal advice.
