# Validation script for the deploy guide (#42)

This script has not been run yet. It was written from the guide, issue #42 and the repository at commit `e87cea8`. Its companion for the onboarding guide is [`onboarding-de-tenant.md`](onboarding-de-tenant.md).

## What it is for

Issue [#42](https://github.com/romulospl/stellytics/issues/42) has two acceptance criteria:

1. Document region, prerequisites, secrets configuration, DNS, TLS, backup, restore and monitoring.
2. "An operator who did not write the guide can deploy the application and verify ingestion, dashboards and alerts by following the document."

The first is a reading check (the guide has a section for each item). The second needs someone outside to run it. This script is what that person fills in, and the report in section 7 is the evidence that stays in the repository.

Section references below point to the sections of the guide. It has two tracks, and the script has one table per track:

- **Track A, existing VPS** (`init.sh`, `deploy.sh`, `infra/compose/prod-vps.yaml`): no backup, no monitoring.
- **Track B, Hetzner** (OpenTofu, Ansible, `infra/compose/prod.yaml`): backup, restore and external monitoring.

Criterion 2 mentions alerts, and only Track B has them. Whether the criterion covers only Track B is a maintainer decision (see section 8).

## 1. Rules for running it

1. The operator must **not have written the guide** and must not work on the project. They follow **only the guide**. The observer takes notes and does not explain or fix anything live. Any help is recorded (step, what was said, how long it took).
2. Record the **guide commit** (`git rev-parse HEAD`) and the exact repository or fork used. The result only holds for that version.
3. Mark each step: **OK** (no help), **OK with help** (becomes a finding), **Failed** (could not do it even with help) or **N/A**.
4. Note the **UTC time** of each step and of each wait (certificate, first boot, image build, data in the dashboard). The guide gives few time limits; the script measures them.
5. **Secrets stay out of the report.** Never paste `.env.production`, `secrets.yml`, tokens, access keys, passwords, the `walg_libsodium_key`, the Umami API key or ingest keys. Mask IPs and domains unless the operator agrees to publish them.
6. Steps marked ● are **required**; the rest are optional.
7. **Cost and cleanup.** Track B creates billable Hetzner resources (two servers, a volume, an Object Storage bucket). Track A needs a VPS. Use a throwaway account or VPS and note what was created. The guide says `tofu destroy` fails on purpose (`delete_protection`, `prevent_destroy`) and does not explain how to tear down; step B24 records how the operator handled it.
8. **Destructive steps** are marked "(disruptive)". They stop a service or touch data. Run them only on the new, empty deploy, never on a real production.

## 2. Before starting (for whoever runs the script)

| Item | Value |
|---|---|
| Track (A, B or both) | |
| Guide commit and repository or fork | |
| Operator (role and experience, no name if they prefer) | |
| Provider and plan (VPS, or Hetzner project) | |
| Domain and DNS provider | |
| SMTP provider | |
| Start date and time (UTC) | |

## 3. Track A, existing VPS

| ID | What to do (guide section) | Expected result | Result | Notes and time (UTC) |
|---|---|---|---|---|
| A1 ● | Read the minimal-path prerequisites and check each one: root access, Debian or Ubuntu (`amd64` or `arm64`) with `git`, four DNS names resolving to the VPS, ports 80 and 443 open, a real SMTP (Minimal path) | The operator knows what to prepare before starting | | |
| A2 ● | Clone the repository and enter the folder (Minimal path) | Code in place, no path guessing | | |
| A3 ● | `sudo ./init.sh` | Docker and Compose installed; `.env.production` created with random passwords and permission `0600` (to be confirmed) | | |
| A4 ● | Fill in `.env.production`: four domains, `ACME_EMAIL`, `SMTP_URL`, `MAIL_FROM` (Minimal path) | The operator knows the `SMTP_URL` format and what `PORTAL_ALIAS_HOST` is | | |
| A5 ● | `sudo ./deploy.sh`. Note the build time | Images built, stack up, `ps` lists the services healthy (to be confirmed) | | |
| A6 ● | Check the stack with the `docker compose ... ps` command from the guide | portal, ingest-web, collector, rollup, umami, postgres and caddy running | | |
| A7 ● | Open the admin tunnel and change the Umami password (Minimal path, step by step) | The operator understands where each command runs (own machine vs VPS) and that the tunnel logs in as `root` | | |
| A8 ● | Generate the Umami service-account key, put it in `UMAMI_API_KEY`, run `sudo ./deploy.sh` again | Portal can create sites in Umami | | |
| A9 ● | Run the checks from section 6 (`curl` block, from any machine) | portal `{"ok":true}`; ingest `/healthz` ok; `/c` returns a challenge; `/api/send` 404 on both hosts; `POST /v1/metrics` without a key returns 401 (to be confirmed) | | |
| A10 ● | TLS: open the four hostnames in a browser | Valid certificates (Let's Encrypt) for all four; alias redirects to the portal | | |
| A11 | From outside the VPS, `nc -vz <vps-ip> 443` and `nc -vz <vps-ip> 5432` (section 6) | 443 opens; 5432 fails | | |
| A12 ● | Browser test from section 6: sign up, confirm the email, create the project, register a site (`/sign-up`, `/onboarding`, `/sites`) | Confirmation email arrives; project and site created. If the email does not arrive, the guide points to the SMTP settings and the portal logs | | |
| A13 ● | Install the tracker script on a test page and visit it; look at the site dashboard (section 6, step 2) | Visit shows in the dashboard "in a few minutes"; note the time | | |
| A14 ● | Operator account and `/operator` (section 6, step 3) | **Watch this step:** the guide creates the first operator only in Track B (step 5, item 4). Note whether Track A explains it and what the operator did | | |
| A15 ● | Create a service and an ingest key, send OTLP metrics to `https://<api_host>`, look at the service dashboard (section 6, step 4; `examples/api-backend/README.md`) | Requests show within 1 to 2 minutes | | |
| A16 | Alerts: does Track A mention any alerting or monitoring? | No (the minimal path has none). Record what the operator expected and did | | |
| A17 | Restart durability (disruptive): `docker compose ... down` (never `-v`) and `./deploy.sh`; then check the data is still there | Volumes preserved; site, account and metrics still exist | | |
| A18 | Update: pull new code and run `sudo ./deploy.sh` again (README, production deployment section) | Existing volumes preserved | | |

## 4. Track B, Hetzner

| ID | What to do (guide section) | Expected result | Result | Notes and time (UTC) |
|---|---|---|---|---|
| B1 ● | Read "What you need" and prepare the accounts: Hetzner project with Object Storage, domain, SMTP, an email for Let's Encrypt, a GitHub account that can publish releases (or a fork) | Nothing missing on the list; note anything the operator had to guess | | |
| B2 ● | Install the tools and check the versions: `git`, `ssh`, `openssl`, OpenTofu 1.13.2 or newer (or the container), `ansible-core` 2.21, Docker with buildx, `jq`, Node 24 and pnpm (corepack) | All tools present; any version mismatch noted | | |
| B3 ● | `git clone`, `corepack enable && pnpm install` (What you need) | Works from the repository root | | |
| B4 ● | Create the Hetzner token and Object Storage credentials (Secrets) | The operator finds each place in the console (the guide warns the names may have changed) | | |
| B5 ● | Generate every secret in `secrets.yml` with `openssl rand -hex 32`; check `walg_libsodium_key` has exactly 64 hex characters and `ingest_proof_secret` at least 32 (Secrets) | Playbook later accepts all fields | | |
| B6 ● | Step 1: copy `production.tfvars` to `production.secret.tfvars` and replace every `CONFIRA` (`server_type`, `monitor_server_type`, `backup_bucket`, `admin_ssh_public_key`, `admin_cidrs`) | `tofu validate` and `plan` pass; the operator picks valid `amd64` server types | | |
| B7 ● | Step 1: `tofu init`, `tofu apply`, `tofu output`. Note the time and the outputs saved | VM, volume, firewall, bucket and monitor VM created; outputs listed | | |
| B8 ● | Step 1, item 3: wait and `ssh root@<server_ipv4> true`, accepting the host key | SSH works | | |
| B9 ● | Step 2: create the four A (and AAAA) records with the operator's own domain | Records resolve to the VM; the operator does not copy the `stellytics.xyz` names | | |
| B10 ● | Step 3: publish a release, wait for the workflow, make the four packages public, get the four digests | `release.yml` green; the four packages public; four digests in hand | | |
| B11 ● | Step 4, items 1 and 2: fill in `inventory/production.yml`, `secrets.yml` and `group_vars/all.yml` (hosts, `acme_email`, `mail_from`, the four images by digest, `walg_s3_prefix`) | Nothing left with the original values (`example.org`, `*.stellytics.xyz`, `CONFIRA`) | | |
| B12 ● | Step 4, item 3: `ansible stellytics -m ping`, then `ansible-playbook site.yml -e @secrets.yml`. Note the time | Ping works; playbook finishes with the stack up; `cosign verify` passes (to be confirmed) | | |
| B13 ● | Step 5: open the tunnel, change the Umami admin password, create the service-account key, put it in `umami_api_key`, run the playbook again | Portal can create sites in Umami | | |
| B14 ● | Step 5, item 4: create the first operator (`operator:create`) | Operator account exists with a confirmed email | | |
| B15 ● | Step 5, item 5: first base backup (`systemctl start stellytics-backup`) and `wal-g backup-list` | One backup listed | | |
| B16 ● | Step 6: `curl` block, `nc` block, and the commands on the VM (`ps`, `port caddy`, `port postgres`, `systemctl list-timers`) | As listed in the guide; 5432 only on `127.0.0.1` (to be confirmed) | | |
| B17 ● | Step 6, browser test: sign up, project, site, install the script, see the visit | Same as A12 and A13 | | |
| B18 ● | Step 6, items 3 and 4: operator at `/operator`, service, ingest key, OTLP to `https://<api_host>` | Project in `/operator`; service dashboard shows requests | | |
| B19 ● | Monitoring: configure `monitor_*` variables, run `monitoring.yml`, check `probe_success` for portal, gateway and `ingest-api` through the tunnel | `probe_success` equals 1 for the three targets | | |
| B20 ● | Alerts (disruptive, only on the new empty deploy): stop `ingest-web` for about 2 minutes, wait for the `EndpointDown` email, start it again | Email arrives; service recovers; note the delay | | |
| B21 | Dead-man URL: configure `monitor_deadman_url` (Monitoring, recommended item 3) | The operator understands why it is recommended | | |
| B22 | Restore rehearsal on a disposable VM, following "Restoring the database" (disruptive, never on the live one) | Database restored; portal works; note the time | | |
| B23 | Update and rollback: publish a second release, change the digests, rerun the playbook; then roll back (Updating the application) | Containers recreated; rollback restores code but not the database, as the guide warns | | |
| B24 | Teardown: how did the operator remove what was created? The guide says `tofu destroy` fails on purpose | Record what they did; if the guide gave no steps, that is a finding | | |

## 5. Checks to do at the end of either track

| ID | What to check | Expected result | Result | Notes |
|---|---|---|---|---|
| E1 ● | Data residency: where do the data live? (Where the data lives) | Track B: Hetzner Nuremberg (`nbg1`), as the guide says. Track A: the operator's VPS | | |
| E2 ● | Secrets: is any secret in a file tracked by git? (`git status`, `.gitignore`) | `secrets.yml`, `inventory/production.yml`, `production.secret.tfvars` and `.env.production` are ignored | | |
| E3 | Terms: list every word, acronym or tool the operator did not understand | A list, even if empty | | |
| E4 | Dashboards: operator checked the tenant dashboards (site and service) and the operator view | They show the data from A13/A15 or B17/B18 | | |

## 6. Total time and effort

Record, per track: time from "started reading" to the first dashboard data, hands-on time vs waiting time, number of times the operator left the guide (search, asking someone), and what they searched for.

## 7. Report template

Copy to `docs/validation/reports/deploy-<track>-<YYYY-MM-DD>.md` or to a comment on issue #42. Fill in only what the operator agreed to publish.

```markdown
# Validation report: deploy

- Track: A (existing VPS) | B (Hetzner)
- Guide validated: commit <sha> of docs/deploy.md, repository or fork <name>
- Script: commit <sha> of docs/validation/deploy.md
- Operator: <role and experience, or "anonymous">
- Provider and plan: <VPS or Hetzner plan>
- Period: <start UTC> to <end UTC>
- Observer: <name>

## Result

| Required steps | OK | OK with help | Failed |
|---|---|---|---|
| A1 to A10, A12, A13, A14, A15 (track A) or B1 to B20 (track B), plus E1, E2 | n | n | n |

Conclusion: validated | validated with caveats | not validated
Total time until the first dashboard data: <hh:mm>

## Steps

(Paste the filled step table, without secrets, IPs or domains.)

## Findings in the guide

| # | Step | Guide excerpt | Problem | Severity (high, medium, low) | Proposed fix |
|---|---|---|---|---|---|

## Help received

| Step | What the operator asked | What was answered | Already in the guide? |
|---|---|---|---|

## Evidence

- Output of the section 6 checks (`curl` and `nc`), with domains and IPs masked
- Screenshot of a tenant dashboard with the test visit and of the service dashboard with the test requests
- Screenshot or text of the `EndpointDown` alert email (track B), headers masked
- `wal-g backup-list` output (track B)
- Information the operator agreed to publish: <list>

## Operator's opinion

- Guide clarity, from 1 to 5:
- Where they got stuck or doubtful:
- A term they did not understand:
- What was missing:
- What they would not do alone in production:
```

## 8. How the result feeds the #42 criterion

Proposal, for the maintainers to confirm:

1. The second criterion counts as met when there is at least one report for **Track B** with every required step (●) marked **OK**. Track B is the only track with monitoring and alerts, which the criterion asks to verify.
2. A Track A report adds evidence for the minimal path. Whether Track A also needs a full pass, or the alerts part is excused for it, is for the maintainers to decide and for the guide to state.
3. A step marked **OK with help** counts as a guide failure: it becomes a finding, a fix PR, and the step is repeated before it counts.
4. Reports stay in the repository, so #42 and epic #7 can point to the evidence.

## 9. Known limits

- No step has been executed. The "expected results" come from the text of the guide and the files; those marked "to be confirmed" depend on a real VPS or Hetzner account.
- Gaps to watch (not confirmed): Track A has no operator-creation step (A14), no alerts (A16) and no teardown; Track B has no teardown steps (B24).
- Whether the minimal path was ever run on a real VPS is not recorded in the repository.
