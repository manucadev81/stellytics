# Production deployment

There are two paths: a minimal install on an existing VPS, described below, or the managed infrastructure on Hetzner with OpenTofu and Ansible ([#14](https://github.com/romulospl/stellytics/issues/14)), detailed in the following sections.

## Minimal path: existing VPS

This path is for a Debian/Ubuntu VPS. It needs no Hetzner account and no S3 storage: Postgres and the certificates live in local Docker volumes. **There is no automatic external backup**; if the VPS or its disk is lost, the data may be lost too.

Prerequisites:

- root access to the VPS (the admin tunnel, further below, logs in over SSH as `root`);
- Debian or Ubuntu, `amd64` or `arm64`, with `git` installed (`sudo apt-get install -y git`);
- four DNS names pointing to the VPS, with A records (and AAAA, if the VPS has IPv6):
  - `PORTAL_HOST`: the portal;
  - `PORTAL_ALIAS_HOST`: an extra name that redirects to the portal (for example, the domain without the `app.`);
  - `INGEST_HOST`: web ingestion; the name must not contain `umami`, `analytics`, `track` or `collect`, which ad blockers block;
  - `API_HOST`: API ingestion (OTLP);
- ports 80 and 443 (TCP) open; 443/UDP, optional, enables HTTP/3;
- a real SMTP server, with authenticated sending, for sign-up confirmation and password reset.

On the VPS, clone the repository (or your fork) into a folder of your choice and run, from inside it:

```bash
git clone https://github.com/romulospl/stellytics.git
cd stellytics
sudo ./init.sh
sudo nano .env.production
sudo ./deploy.sh
```

`init.sh` installs Docker/Compose and generates random passwords in `.env.production` with permission `0600`. Fill in the four domains, `ACME_EMAIL`, `SMTP_URL` and `MAIL_FROM`; do not change the generated passwords. The `SMTP_URL` format is `smtps://user:password@host:465`, with special characters of the password URL-encoded, and the domain of `MAIL_FROM` must be authorized on your SMTP server.

`deploy.sh` builds the portal, gateway, collector and rollup images on the server. Postgres, Umami and Caddy use pinned images. Only Caddy publishes ports; Postgres is reachable only through loopback and Umami only on the internal network. The `docker compose down` command keeps the volumes; **do not use `down -v`**, which deletes the database and the TLS state.

Check the stack on the VPS, from inside the clone folder:

```bash
sudo docker compose --env-file .env.production -f infra/compose/prod-vps.yaml ps
```

On the first start, do these steps on **your own machine**, not on the VPS (you need a clone of the repository and Node 24 or newer; the script only uses Node's own modules, with no `pnpm install`):

1. Open the admin tunnel and leave it open in a terminal: `infra/scripts/admin-tunnel.sh <VPS-IP>`. It brings Umami to `http://127.0.0.1:13001` and Postgres to `127.0.0.1:15432`. The script logs in over SSH as `root`; if root SSH login is disabled on your VPS, open the tunnel by hand with your own user:

   ```bash
   ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:15432:127.0.0.1:5432 -L 127.0.0.1:13001:172.30.0.20:3000 <user>@<VPS-IP>
   ```

2. Open `http://127.0.0.1:13001`, sign in with `admin`/`umami` and change the password.
3. For the portal to create sites in Umami, generate the service-account key, with the tunnel open:

   ```bash
   read -rs UMAMI_ADMIN_PASSWORD && export UMAMI_ADMIN_PASSWORD
   UMAMI_URL=http://127.0.0.1:13001 node apps/portal/scripts/umami-service-account.mjs
   ```

   The script prints the key once. Store it, and do not share it.
4. On the VPS, save the key in `UMAMI_API_KEY` in `.env.production` and run `sudo ./deploy.sh` again.

To check the installation, use [section 6](#6-check-the-production-deployment): the `curl` commands and the browser test apply to this path. Instead of the Docker commands there (`prod.yaml`, in `/opt/stellytics/infra/compose`), use, from inside the clone folder, `sudo docker compose --env-file .env.production -f infra/compose/prod-vps.yaml <command>`; the service names are the same. The certificates live in the Docker volume `caddy-data`, not in `/srv/stellytics/caddy`.

## Managed path: Hetzner

The decisions for this path are in [ADR 0004](adr/0004-hospedagem-e-backup.md); the files are in [`infra/`](../infra/README.md).

> **Scope.** This guide brings up the whole application infrastructure in [`infra/compose/prod.yaml`](../infra/compose/prod.yaml): Caddy, portal, web ingestion gateway, API collector, rollup job, Umami and Postgres with backup, plus external monitoring. Caddy terminates TLS automatically and forwards the hosts over the Docker network. The collector and the rollup job were added on 16 Nov 2026 ([ADR 0012](adr/0012-ingestao-de-api-pelo-caddy.md)). **[#42](https://github.com/romulospl/stellytics/issues/42) is not yet fulfilled:** its acceptance criterion (someone outside the project does the deployment following only the guide) will only be met when someone outside follows the guide on a real account.

## Overview

```mermaid
flowchart LR
  internet((Internet)) -->|80, 443| caddy[Caddy in Docker]
  subgraph vm["VM on Hetzner (nbg1, Germany)"]
    caddy --> portal[Portal]
    caddy --> gw[ingest-web gateway]
    caddy --> collector[OTLP collector]
    portal --> umami[Umami]
    gw --> umami
    portal --> pg[(Postgres + WAL-G)]
    gw --> pg
    umami --> pg
  end
  pg -->|continuous WAL and daily backup, encrypted| s3[(Object Storage nbg1)]
```

- All the application services, including the proxy, run in Docker Compose. Only Caddy publishes 80/443; Postgres publishes only on loopback for the SSH tunnel and the other services stay on the internal network.
- The Postgres data and the Caddy TLS state/certificates live on the Hetzner volume at `/srv/stellytics`.
- Caddy does not write access logs; errors go to the container log without sensitive headers or URLs.

## Where the data lives (data residency)

| What | Where |
|---|---|
| VM and volume with Postgres (`stellytics` and `umami` databases) | Hetzner, Nuremberg (`nbg1`), Germany |
| Postgres backups (WAL and base backups), encrypted on the client | Hetzner Object Storage, Nuremberg (`nbg1`), Germany |

The location is the OpenTofu variable `location`. If you change it, also change `walg_s3_endpoint` and `walg_s3_region` in Ansible.

## What you need

**Accounts and services:**

| What | What for |
|---|---|
| A Hetzner Cloud project, with Object Storage enabled | VMs, volume, firewall and the backup bucket |
| A domain whose DNS you control (any provider) | Four names: portal, portal alias (redirects to the portal), web ingestion and API ingestion (step 2) |
| An SMTP provider (authenticated sending) | Email confirmation at sign-up and password recovery in the portal. **Without it nobody can sign up**, because a confirmed email is required. URL format: `smtps://user:password@host:465` |
| An email address for Let's Encrypt (`acme_email`) | Automatic TLS certificate notices from Caddy |
| A GitHub account allowed to publish releases of the repository (or your fork) | Build the signed images (step 3) |
| Optional: a dead-man's-switch service (healthchecks.io or similar) | Warns if the monitoring VM itself goes down |

**Tools on your machine:**

- `git`, `ssh` and `openssl`; optionally the `hcloud` CLI (for `hcloud server-type list`; without it, see the types in the Hetzner console);
- OpenTofu 1.13.2 or newer (or the container `ghcr.io/opentofu/opentofu:1.13.2`) and Ansible (ansible-core 2.21; the deployment only uses `ansible.builtin`, with no extra collections);
- Docker with buildx and `jq`, to read the image digests (or take the digests from the package page on GitHub);
- Node 24 or newer and pnpm 10.34.5 (`corepack enable`), for the admin commands in step 5.

Clone the repository and install the dependencies. All the commands below start from the root of the clone:

```bash
git clone https://github.com/romulospl/stellytics.git && cd stellytics
corepack enable && pnpm install
```

For the managed path on Hetzner, install OpenTofu and `ansible-core` as in the list above. The `init.sh` and `deploy.sh` scripts belong to the minimal VPS path; they install Docker locally and do not replace the Ansible playbooks in this section.

**Secrets** (never in the repository):

| Secret | Where it is used | How to get it |
|---|---|---|
| `HCLOUD_TOKEN` | OpenTofu | Hetzner console → project → Security → API tokens (read and write) |
| `TF_VAR_object_storage_access_key` and `TF_VAR_object_storage_secret_key` | OpenTofu (creates the bucket) and Ansible (`walg_s3_access_key` and `walg_s3_secret_key`) | Hetzner console → project → Object Storage → S3 credentials |
| `secrets.yml` | Ansible | Copy `infra/ansible/secrets.example.yml` and fill it in (see below) |

In `secrets.yml`, all these fields are **required** (the playbook stops if any is empty): `postgres_password`, `umami_db_password`, `stellytics_db_password`, `stellytics_ingest_db_password`, `stellytics_collector_db_password`, `umami_app_secret`, `portal_auth_secret`, `ingest_proof_secret`, `smtp_url`, `walg_s3_access_key`, `walg_s3_secret_key` and `walg_libsodium_key`. Generate each password or key with `openssl rand -hex 32` (64 characters; `ingest_proof_secret` needs at least 32, and `walg_libsodium_key` exactly 64 hexadecimal characters). `umami_api_key` starts empty and only exists after step 5. `secrets.yml` and `inventory/production.yml` are in `.gitignore`.

The `walg_libsodium_key` key encrypts the backups: **without it, no backup can be restored**. Keep a copy in the team's password vault, outside the VM.

**If you use a fork** (instead of the images from `romulospl/stellytics`): replace `ghcr.io/romulospl/stellytics/...` with your own images and `cosign_identity_regexp` with the path of your fork's `release.yml`, in `group_vars/all.yml`. The playbook refuses images whose signature does not match that identity.

## 1. Provision the infrastructure (OpenTofu)

1. Copy `infra/opentofu/production.tfvars` to `infra/opentofu/production.secret.tfvars` (that name is in `.gitignore`) and replace every `CONFIRA` value: `server_type` and `monitor_server_type` (check with `hcloud server-type list`; pick types with the same architecture, x86 or Arm, as the published images, which are `linux/amd64`), `backup_bucket` (a name unique across all of Hetzner), `admin_ssh_public_key` and `admin_cidrs` (the IPs you administer from, never `0.0.0.0/0`). If your IP changes, update `admin_cidrs` and apply again, or SSH stays closed.
2. Run (with the `-var-file` you created):

   ```bash
   export HCLOUD_TOKEN=...
   export TF_VAR_object_storage_access_key=...
   export TF_VAR_object_storage_secret_key=...
   tofu -chdir=infra/opentofu init
   tofu -chdir=infra/opentofu apply -var-file=production.secret.tfvars
   tofu -chdir=infra/opentofu output
   ```

   The outputs give the IPs of the VM (`server_ipv4`, `server_ipv6`) and of the monitoring VM (`monitor_ipv4`), the volume device (`data_volume_device`) and the backup endpoint and prefix (`backup_s3_endpoint`, `backup_s3_prefix`). Keep `server_ipv4`, `server_ipv6`, `monitor_ipv4`, `data_volume_device` and `backup_s3_prefix`: steps 2 and 4 use all of them. `backup_s3_endpoint` only changes if you change the `location`.
3. Wait a minute or two: cloud-init updates the VM on first boot. Check that SSH works: `ssh root@<server_ipv4> true` (accept the host key; Ansible refuses unknown hosts, `host_key_checking = True`).

The VM, the volume and the bucket have removal protection (`delete_protection`, `prevent_destroy`): `tofu destroy` fails on purpose.

## 2. DNS

Create A records (and AAAA, with the IPv6) pointing to the VM:

- `<portal_alias_host>` (redirects to the portal);
- `<portal_host>` (portal);
- `<ingest_host>` (web ingestion; the name must not contain terms that ad blockers block, such as `umami`, `analytics`, `track` and `collect`);
- `<api_host>` (OTLP/HTTP and gRPC on 443).

Choose the four names in your own domain; in step 4 you copy them to `group_vars/all.yml`. The values that file ships with (`stellytics.xyz`, `app.stellytics.xyz`, `e.stellytics.xyz`, `otlp.stellytics.xyz` and the emails under `example.org`) belong to the original project and must be replaced.

Use the IPs `server_ipv4` (A) and `server_ipv6` (AAAA). Caddy in Compose requests and renews certificates automatically once the four names resolve to the VPS and ports 80/443 are open. There is no need to install a proxy or an ACME client on the host.

## 3. Publish the images

The deployment only accepts images published by the release workflow, signed with cosign and referenced by digest.

1. Publish a release on GitHub (`vX.Y.Z`). The `release.yml` workflow generates the SBOM and publishes to GHCR one image for each `Dockerfile` in `apps/*` and `services/*`, signed with cosign. The deployment uses four: `ghcr.io/romulospl/stellytics/portal`, `ingest-web`, `collector` and `rollup`, with the tag **without the `v`** (`X.Y.Z`); each one goes by digest in `portal_image`, `ingest_web_image`, `collector_image` and `rollup_image`. Follow the workflow in Actions until all jobs are green.
2. **Make the packages public.** New GHCR packages start private, and the VM pulls the images and checks the signature without `docker login`. In GitHub → Packages → each of the four packages (`stellytics/portal`, `stellytics/ingest-web`, `stellytics/collector` and `stellytics/rollup`) → Package settings → Change visibility → Public. Without this, step 4 fails at `cosign verify` or at `docker compose up` with `unauthorized`.
3. Get the digests:

   ```bash
   for name in portal ingest-web collector rollup; do
     digest=$(docker buildx imagetools inspect ghcr.io/romulospl/stellytics/$name:X.Y.Z --format '{{json .Manifest}}' | jq -r .digest)
     echo "ghcr.io/romulospl/stellytics/$name@$digest"
   done
   ```

## 4. Deploy (Ansible)

1. In `infra/ansible/`:
   - copy `inventory/production.example.yml` to `inventory/production.yml` and fill it in with the OpenTofu outputs (`server_ipv4` and `data_volume_device` on the `stellytics` host; `monitor_ipv4` on the `monitor` host, used only in the monitoring step);
   - copy `secrets.example.yml` to `secrets.yml` and fill it in.
2. In `group_vars/all.yml`, fill in:
   - the hosts (`portal_host`, `portal_alias_host`, `ingest_host`, `api_host`), `acme_email` and `mail_from` (sender of the portal's emails; its domain must be authorized on your SMTP server);
   - `signup_approval` (`false` by default): with `true`, every new project stays `pending` until an operator approves it at `/operator/approvals` ([ADR 0007](adr/0007-cadastro-com-aprovacao.md)). Turning it on or off only applies to new sign-ups and requires re-applying the playbook (it restarts the portal); projects already pending stay in the queue;
   - `portal_image`, `ingest_web_image`, `collector_image` and `rollup_image`, in the format `ghcr.io/romulospl/stellytics/portal@sha256:<digest>` (the playbook refuses an image without a digest);
   - `walg_s3_prefix`, with the `backup_s3_prefix` output. `walg_s3_endpoint` and `walg_s3_region` are already correct for `nbg1`.
3. Check the connection and run, from `infra/ansible/`:

   ```bash
   ansible stellytics -m ping
   ```

   ```bash
   ansible-playbook site.yml -e @secrets.yml
   ```

The first time it takes a few minutes (updates, Docker, building the Postgres image with WAL-G). The playbook prepares the VM and brings the application up:

- updates the VM and turns on automatic security updates;
- installs Docker (with the repository key verified) and cosign;
- mounts the volume;
- copies the Compose file, the Caddyfile and the `.env` (permission 600) to `/opt/stellytics`;
- verifies the image signatures;
- brings the stack up (the portal migrations run during this start) and enables the daily backup timer.

On this first start, `UMAMI_API_KEY` is empty: the portal works (sign-in, sign-up), but it cannot create sites in Umami yet. Step 5 fixes that.

The playbook can run again as many times as needed; with no change, it changes nothing.

## Updating an existing `notify` production

The first `ansible-playbook site.yml -e @secrets.yml` of this version performs the rename from `notify` to `stellytics` in a maintenance window. Before running it:

1. in `secrets.yml`, rename the keys `notify_db_password`, `notify_ingest_db_password` and `notify_collector_db_password` to the equivalent `stellytics_*` ones, keeping the values if you do not intend to change the passwords in that window;
2. change the host group in the inventory from `notify` to `stellytics`;
3. run `tofu plan` and check that the resources `hcloud_server.notify` and `hcloud_firewall.notify` show up as moved, with no VM recreation.

The playbook stops the `notify` stack, turns off the old backup timer, unmounts the volume from `/srv/notify`, mounts it at `/srv/stellytics`, renames the database and the PostgreSQL roles and only then starts the new services. It interrupts traffic during this stage. If both databases or role pairs already exist, it stops without choosing which data set to keep.

## 5. First start (managed Hetzner path)

1. **Admin tunnel** (leave it open in a terminal):

   ```bash
   infra/scripts/admin-tunnel.sh <vm-ip>
   ```

   Postgres at `127.0.0.1:15432`, Umami at `http://127.0.0.1:13001`. None of this is exposed to the internet.
2. **Change the Umami administrator password.** It starts as `admin`/`umami` (reachable only through the tunnel). Open `http://127.0.0.1:13001` through the tunnel and change it, before any other step.
3. **Umami service account** (the portal uses its key), from the clone root and with the tunnel open. The `read` line works in bash and zsh; type the new password from step 2:

   ```bash
   read -rs UMAMI_ADMIN_PASSWORD && export UMAMI_ADMIN_PASSWORD
   UMAMI_URL=http://127.0.0.1:13001 pnpm --filter @stellytics/portal umami:service-account
   ```

   The key printed on standard output goes into `umami_api_key` in `secrets.yml`. Run the playbook again (`ansible-playbook site.yml -e @secrets.yml`): it recreates only the portal with the key.
4. **First SCF operator** (SCF is the Stellar Community Fund; the account has the global view and starts with the email already confirmed), also with the tunnel open. The `read` line works in bash and zsh; the password needs at least 12 characters:

   ```bash
   read -rs OPERATOR_PASSWORD && export OPERATOR_PASSWORD
   DATABASE_URL='postgresql://stellytics:<stellytics_db_password>@127.0.0.1:15432/stellytics' \
     pnpm --filter @stellytics/portal operator:create --email <email> --name "<name>"
   ```

   Other operators come from `operator:promote` (an existing account that does not belong to a project) with the same `DATABASE_URL`.
5. **First base backup**, without waiting for the timer:

   ```bash
   ssh root@<vm-ip> systemctl start stellytics-backup
   ```

   Check with the command below (the same one as in the Backup section): one backup should appear.

   ```bash
   cd /opt/stellytics/infra/compose
   docker compose -f prod.yaml exec -u postgres postgres wal-g backup-list
   ```

## 6. Check the production deployment

Without signing in, from any machine:

```bash
curl -fsS https://<portal_host>/api/auth/ok
curl -fsS https://<ingest_host>/healthz
curl -fsS "https://<ingest_host>/c?s=00000000-0000-4000-8000-000000000000"
curl -s -o /dev/null -w '%{http_code}\n' https://<ingest_host>/api/send
curl -s -o /dev/null -w '%{http_code}\n' https://<portal_host>/api/send
curl -s -o /dev/null -w '%{http_code}\n' -X POST -H 'Content-Type: application/json' -d '{}' https://<api_host>/v1/metrics
```

Expected results, in order:

- the portal answers `{"ok":true}`;
- the gateway answers `ok`;
- the challenge request returns something like `{"t":...,"d":12}`;
- `/api/send` on the ingestion host returns 404: Umami has no public route;
- `/api/send` on the portal host also returns 404: this is the portal, not Umami;
- `POST /v1/metrics` without a key on the API host returns 401: the collector requires an ingest key.

From outside the VM (the VM's Docker runs with `userland-proxy` off, so `ss` does not show a process listening on these ports; check from the outside and through the Docker rules):

```bash
nc -vz -w3 <vm-ip> 443
nc -vz -w3 <vm-ip> 5432
```

Port 443 should open (Caddy in Docker). Port 5432 should fail: Postgres only listens on loopback and the firewall does not open the port.

On the VM (`ssh root@<vm-ip>`):

```bash
cd /opt/stellytics/infra/compose
docker compose -f prod.yaml ps
docker compose -f prod.yaml port caddy 80
docker compose -f prod.yaml port caddy 443
docker compose -f prod.yaml port postgres 5432
systemctl list-timers stellytics-backup.timer
```

`ps` should list the services running, with the upstreams only on the internal Docker network. `port caddy 80` and `port caddy 443` show the public HTTP and HTTPS ports. `port postgres 5432` should show `127.0.0.1`.

In the browser, the test that proves the whole:

1. At `https://<portal_host>/sign-up`, create an account with an email of yours, confirm through the link you receive (if the email does not arrive, the problem is `smtp_url`/`mail_from`: `docker compose -f prod.yaml logs portal`), create the project at `/onboarding` and register a site at `/sites`.
2. Install the script on the site (the gateway serves `https://<ingest_host>/s/v<version>/n.js`; versions and SRI hashes in [`packages/tracker/published/sri.json`](../packages/tracker/published/sri.json); install snippets in [`examples/README.md`](../examples/README.md)), open a page and, within a few minutes, see the visit in the site dashboard.
3. Sign in with the operator account at `/operator` and check that the project shows up.
4. At `/services`, create a service and an ingest key, point a backend at `https://<api_host>` (OTLP/HTTP; guide in [`examples/api-backend/README.md`](../examples/api-backend/README.md)) and see the requests in the service dashboard within a minute or two.

If something fails, each service's logs are in `docker compose -f prod.yaml logs <service>` (caddy, portal, ingest-web, collector, rollup, umami, postgres). The certificate state lives in `/srv/stellytics/caddy`.

## Updating the application

1. Publish a new release (step 3) and make the new packages public, if it is the first time for any of them.
2. Change the digests in `group_vars/all.yml` and run `ansible-playbook site.yml -e @secrets.yml`. The playbook verifies the signature and recreates only the containers whose image changed. The portal migrations run on start (`RUN_MIGRATIONS=true`).
3. Repeat the check from step 6.

**Before a version with a new migration**, force a base backup (`systemctl start stellytics-backup`): migrations only move forward.

**Rolling back.** Put the digests of the previous version in `group_vars/all.yml` and run the playbook again. This rolls back the code, **not the database**: if the new version brought a migration, the schema stays the new one, and the old version may not work with it. In that case, restore the database from the backup taken before the update (section "Restoring the database") before starting the old image. Versions of the tracking script already published stay available at `/s/` (they are immutable), so the tenants' pages do not break.

**Changing a secret.** Changing a password in `secrets.yml` only updates the `.env`; the database users are created once, on the empty volume, and keep the old password. In order:

1. Edit the value in `secrets.yml`.
2. Run `ansible-playbook site.yml -e @secrets.yml`. Compose recreates the containers whose environment changed (Postgres, if a database password changed) and delivers the new `.env`. If the playbook's `--wait` complains about an unhealthy service, that is expected until step 3: run the playbook again at the end.
3. On the VM, in `/opt/stellytics/infra/compose`, update the password in the database:
   - `stellytics_ingest_db_password` and `stellytics_collector_db_password`: re-apply the script (idempotent): `docker compose -f prod.yaml exec -T postgres sh < /opt/stellytics/infra/compose/postgres/init/02-ingest-roles.sh`;
   - `stellytics_db_password` and `umami_db_password`: `docker compose -f prod.yaml exec -T postgres psql -U postgres -c "ALTER ROLE stellytics PASSWORD '<new>'"` (and `ALTER ROLE umami ...`);
   - `postgres_password`: `ALTER ROLE postgres PASSWORD '<new>'` in the same way.
4. Restart whatever holds the connection: `docker compose -f prod.yaml restart portal umami ingest-web`.

Between recreating Postgres (step 2) and the `ALTER ROLE` (step 3), the portal, Umami and the gateway have no connection to the database: do it outside peak hours. The gateway drops the events from that interval.

Effect of the other secrets: `portal_auth_secret` invalidates all portal sessions (everyone signs in again); `umami_app_secret` invalidates the Umami sessions (if the portal starts receiving 401 from it, generate another key with `pnpm --filter @stellytics/portal umami:service-account` and update `umami_api_key`); `ingest_proof_secret` invalidates the challenges in circulation, and browsers ask for a new one on the next load or within 5 minutes, with a few events dropped in the meantime. The `walg_libsodium_key` cannot be changed: the old backups would no longer open.

## Backup

- **Continuous WAL:** Postgres sends each WAL segment to Object Storage through `archive_command`, at most 60 seconds after the write (`archive_timeout`).
- **Daily base backup** at 03:15 UTC, by the `stellytics-backup.timer` timer. It keeps the 7 newest (`walg_retain_full`).
- **See the backups:**

  ```bash
  cd /opt/stellytics/infra/compose
  docker compose -f prod.yaml exec -u postgres postgres wal-g backup-list
  ```

- **Run one now:** `systemctl start stellytics-backup`.
- Restoring is tested on every PR by the CI `infra` job (`infra/compose/test/prod-stack-test.sh`): it restores into a new container and checks that what was written after the base backup also came back.

## Restoring the database

**Maintenance window:** the restore takes down the portal, ingestion (events are dropped) and Umami until it finishes. The new backup is downloaded into an empty directory while the old data stays in `postgres.before-*` on the same volume, so check there is free space for both (`df -h /srv/stellytics`) before starting.

On the VM, as root. The block stops the services, moves the current data aside (do not delete it until you have checked the restore), brings the newest backup and all the WAL after it, starts everything again and takes a new base backup right after the restore:

```bash
cd /opt/stellytics/infra/compose
systemctl stop stellytics-backup.timer
docker compose -f prod.yaml stop portal ingest-web collector rollup umami postgres

mv /srv/stellytics/postgres /srv/stellytics/postgres.before-$(date +%F-%H%M)
mkdir /srv/stellytics/postgres

docker compose -f prod.yaml run --rm --no-deps --entrypoint sh postgres -c \
  'chown postgres:postgres "$PGDATA" && chmod 700 "$PGDATA" && su postgres -c "stellytics-restore LATEST"'

docker compose -f prod.yaml up -d --wait
systemctl start stellytics-backup.timer
systemctl start stellytics-backup
```

On start, Postgres applies the archived WAL and opens the database on a new timeline; WAL-G keeps archiving from it.

**Going back to a point in time** (for example, before an error at 02:00): a base backup already contains everything that happened up to when it finished, so the starting point must be a backup **completed before** the chosen time. List the backups and use the name of the last one that finished before it:

```bash
docker compose -f prod.yaml run --rm --no-deps postgres wal-g backup-list --detail
```

Then run this command in place of the `stellytics-restore LATEST` command from the block above:

```bash
docker compose -f prod.yaml run --rm --no-deps \
  -e BACKUP=<backup-name> -e TARGET='2026-11-14 01:59:00+00' --entrypoint sh postgres -c \
  'chown postgres:postgres "$PGDATA" && chmod 700 "$PGDATA" && su postgres -c "stellytics-restore \"$BACKUP\" \"$TARGET\""'
```

`stellytics-restore` discards the recovery targets that came inside the backup (from a previous restore), so each restore uses only the requested target.

## OpenTofu state

The state lives in `infra/opentofu/terraform.tfstate`, on the machine of whoever applies (outside git, because it holds infrastructure data). To share it between people, keep it in the same Object Storage: add the `backend` block **inside** the `terraform` block that already exists in `versions.tf`, keeping `required_version` and `required_providers` as they are:

```hcl
terraform {
  backend "s3" {
    bucket                      = "<state-bucket>"
    key                         = "stellytics/production.tfstate"
    region                      = "nbg1"
    endpoints                   = { s3 = "https://nbg1.your-objectstorage.com" }
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_s3_checksum            = true
    use_path_style              = true
  }
}
```

Then run `tofu init -migrate-state`. The credentials come from `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`.

## Monitoring

A separate VM, in another data center (`fsn1`), probes the portal and ingestion from the outside every 15 seconds and sends an email when something goes down ([ADR 0005](adr/0005-monitoramento-externo.md)). It is created by the same `tofu apply` (output `monitor_ipv4`).

Do this after the deployment (step 4) and DNS, otherwise the monitor starts out alerting.

1. In `inventory/production.yml`, confirm the `monitor` host with `monitor_ipv4` (see the `.example`). Accept its SSH key once: `ssh root@<monitor_ipv4> true`.
2. In `group_vars/all.yml`, fill in `monitor_alert_email`, `monitor_smtp_smarthost` (`host:port`), `monitor_smtp_from` and, if the SMTP server asks for a login, `monitor_smtp_username`. In `secrets.yml`, fill in `monitor_smtp_password`.
3. **Recommended:** set up an external dead-man's-switch service (any one that receives a periodic POST and warns when it stops) and put its URL in `monitor_deadman_url`. Without it, if the monitoring VM goes down, nobody finds out.
4. Run, from `infra/ansible/`:

   ```bash
   ansible-playbook monitoring.yml -e @secrets.yml
   ```

| Alert | When |
|---|---|
| `EndpointDown` | Portal, web gateway or API ingestion with no 2xx response for 1 minute |
| `TLSCertificateExpiringSoon` | Certificate expiring in less than 14 days (automatic renewal failed) |
| `ProbeFailing` | Prometheus cannot even ask blackbox to probe a target |
| `ProberDown` | The blackbox exporter, which runs the probes, is down |
| `ReplicationStopped` / `ReplicationMonitorDown` | With replica monitoring enabled: slot missing or not streaming, or collection failing for 2 minutes |
| `ReplicationLagHigh` / `ReplicationSlotWALHigh` | Lag above 256 MiB for 5 minutes or retention above 5 GiB for 2 minutes |

Check that monitoring sees production: in Prometheus (tunnel below), the *Status → Targets* page (or the `probe_success` query) should show `1` for the portal, the gateway and API ingestion (`ingest-api`, a POST without a key that expects the collector's 401). To test the alert email for real, on the production VM run `docker compose -f prod.yaml stop ingest-web`, wait about 2 minutes for `EndpointDown` and start it again (`start ingest-web`). **Stopping `ingest-web` drops the real events that arrive in that interval.** Do the test on the first deployment, before there are tenants, or accept the loss. Alertmanager and Prometheus are only on loopback; to see them, open a tunnel and go to `http://127.0.0.1:9090` and `http://127.0.0.1:9093`:

```bash
ssh -N -L 9090:127.0.0.1:9090 -L 9093:127.0.0.1:9093 root@<monitor_ipv4>
```

**API ingestion:** `monitor_api_url` is already built from `api_host` in `group_vars/all.yml`; keep the two aligned if you change the domain and re-apply `monitoring.yml`. The blackbox probe only confirms that the collector requires a key (401), not that an authorized metric will be stored.

## High availability (optional, steps 1 and 2 of ADR 0009)

> Everything is off by default: with the default values, `site.yml`, `prod.yaml` and the `.env` stay **identical** to before (checked with `docker compose config` and by rendering `env.j2`). This delivers the **configuration** of the replicas; it does **not** deliver automatic failover or the 99.99% and 99.90% targets ([`docs/sla.md`](sla.md)). None of this was applied on a real Hetzner account.

What exists:

| Piece | Files | What for |
|---|---|---|
| Primary with replication | `compose/prod-ha-primary.yaml`, `compose/postgres/init/03-replication.sh`, variable `ha_replication` | `replicator` role, `pg_hba` only for the `ha_replication_cidr` range (`10.50.0.0/16`), WAL limit per slot (`max_slot_wal_keep_size`, 10 GB), Postgres and Umami published on the private IP `ha_private_ip` |
| Postgres replica | `standby.yml`, role `postgres_standby`, `compose/prod-standby.yaml`, `compose/postgres/standby-entrypoint.sh` | `pg_basebackup` + `standby.signal` + replication slot, read-only hot standby |
| Extra ingestion | `ingest.yml`, role `stellytics_ingest`, `compose/prod-ingest.yaml`, `caddy/Caddyfile.ingest` | Only Caddy and the web gateway, reading `ingest_site` (as `stellytics_ingest`) and relaying to the primary's Umami over the private network |
| Manual failover | [`runbooks/failover-do-banco.md`](runbooks/failover-do-banco.md) | Promote the replica, move the stack, what to do with the old primary |

Order (each step is an `ansible-playbook` from `infra/ansible`, with `secrets.yml`):

1. **OpenTofu:** turn on `ha_db_standby` and/or `ha_ingest_replicas` (see `infra/opentofu/variables.tf`; creates paid resources). Fill in the inventory (`inventory/production.example.yml`, groups `ingest` and `standby`) with the public IPs of the VMs and the replica's volume (OpenTofu only exports the replica's private IP; the rest come from the console).
2. **New secret:** `replicator_db_password` in `secrets.yml` (`openssl rand -hex 32`). Keep it like the others.
3. **Primary:** `ansible-playbook site.yml -e @secrets.yml -e ha_replication=true`. It recreates the Postgres container (a short database restart, and the portal, Umami and the gateway lose the connection for a few seconds) and applies `03-replication.sh` to the existing volume (idempotent). Docker only starts the container if the private IP exists on the VM: after rebooting the VM, check that the private network interface came up before Docker.
4. **Replica:** `ansible-playbook standby.yml -e @secrets.yml`. It copies the primary over the private network (the time grows with the database size; in the local test, a database of a few MB took 6 s) and checks `pg_is_in_recovery() = t`.
5. **Check.** On the primary:

   ```bash
   docker compose -f prod.yaml -f prod-ha-primary.yaml exec -T postgres psql -U postgres -Atc \
     "SELECT state, sync_state, pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn) AS bytes_behind FROM pg_stat_replication"
   ```

   On the replica:

   ```bash
   docker compose -f prod-standby.yaml exec -T postgres psql -U postgres -d stellytics -Atc "SELECT pg_is_in_recovery()"
   ```

6. **Monitor the replica (optional, recommended):** the OpenTofu option `ha_db_standby=true` connects the monitoring VM to the HA network at `10.50.0.40`. First publish a signed release containing the `replication-monitor` image and set `monitor_replication_image` to the digest reference; the playbook verifies its signature. Generate `replication_monitor_password` with `openssl rand -hex 32` and keep it in `secrets.yml`. Run `site.yml` and then `monitoring.yml`, both with `-e @secrets.yml -e ha_replication=true -e monitor_replication_enabled=true`. The limited account `stellytics_replication_monitor` gets `pg_monitor` and only enters the `postgres` database from the address `monitor_replication_cidr` (default `10.50.0.40/32`); the connection travels without TLS over the private network. Restrict the private network and keep the password protected. Check in Prometheus `stellytics_replication_scrape_success=1`, `stellytics_replication_slot_exists=1` and `stellytics_replication_streaming=1`. If you promote the replica, update `ha_primary_private_ip` to the new primary and run both playbooks again. The real Hetzner network has not been tested yet.
7. **Extra ingestion:** use the **same** `ingest_proof_secret`, `stellytics_ingest_db_password` and `ingest_web_image` (by digest, verified with cosign) as the primary and run `ansible-playbook ingest.yml -e @secrets.yml`. Caddy on those VMs only publishes `/e`, `/c`, `/s/*` and `/healthz` of the ingestion host.

Limits and cautions:

- **Backup:** only the primary schedules `stellytics-backup`. The replica does not archive WAL while it is a replica (tested: `archived_count = 0`) and does not take base backups; WAL-G keeps working exactly as before on the primary (tested). After a promotion, the new timeline archives to the same prefix; take a base backup soon after.
- **Slot:** a stopped replica holds WAL on the primary up to 10 GB (`ha_replication_max_slot_wal_keep`); beyond that, the slot is invalidated and the replica has to be rebuilt (delete its data directory and run `standby.yml`). With no replica there is no slot and the primary does not keep extra WAL.
- **Network:** Postgres and Umami on the primary start listening on the private IP. The image's pg_hba.conf ends with `host all all all scram-sha-256`, which would leave `postgres`, `umami`, `stellytics` and `stellytics_collector` reachable over that network with only a password; so `03-replication.sh` inserts, before it and for each range of `ha_replication_cidr`, `stellytics_ingest` (only on the `stellytics` database), `replicator` (replication only) and `reject` for everything else (tested: from a client in the range, the four roles are rejected and `stellytics_ingest` and `replicator` work; Compose traffic comes from 172.30.0.0/24 and is not affected). **Remaining risk:** Umami (3000) is reachable from any host on the private network, with no authentication on `/api/send`, and it accepts the client IP header: whoever controls a VM on the network injects events with a fake IP. Mitigation: Hetzner firewall opening 3000 and 5432 only between the known private IPs (today's OpenTofu does not do this; it is pending). The real private network has **not been tested**; in the local test the range is that of a Docker network.
- **TLS of the ingestion replicas:** each VM requests the certificate through HTTP-01. Behind a load balancer that forwards 80 and 443, the challenge lands on any VM and may fail; a certificate on the load balancer or another method is still open (ADR 0009). The load balancer, the floating IP and DNS are **not** configured here.
- **Per-instance limits:** `RATE_LIMIT` and `FINGERPRINT_LIMIT` apply per replica (ADR 0009); with N replicas the effective limit can reach N times. Three gateway variables arrive through `.env` (Compose) or through Ansible variables, all off by default (behavior identical to before):

  | Compose / `.env` | Ansible | Default | When to turn on |
  |---|---|---|---|
  | `INGEST_SITE_STALE_TTL` | `ingest_site_stale_ttl` | `0` | Max. 1 h (e.g. `10m`). With the database down, the gateway serves the last response for an **accepted** site for that long. Trade-off: a tenant removed during the outage keeps accepting events until `SITE_CACHE_TTL` + this value. |
  | `INGEST_RELAY_MAX_INFLIGHT` | `ingest_relay_max_inflight` | `0` (no limit) | Only if Umami saturates and the gateway starts accumulating memory. Under saturation, the gateway answers `204` and logs the drops; timing differences may still give clues (ADR 0011). |
  | `INGEST_RELAY_QUEUE_WAIT` | `ingest_relay_queue_wait` | `250ms` | Only applies with the limit above turned on (0 to 10 s). |
- **Portal and Umami** stay on a single VM, with the database local to it (`postgres:5432` in `prod.yaml`).

## What the guide does not cover yet

- **API collector:** it is in `prod.yaml` and in Caddy ([ADR 0014](adr/0014-infra-em-docker-compose.md)), but has never run on a real account. Caddy does not rate-limit requests per IP and the collector does not rate-limit per key; the container memory cap is `collector_mem_limit` (default `512m`). The extra ingestion VMs (`prod-ingest.yaml`) only have the web gateway: API ingestion stays on a single VM.
- **SCF read API (`/api/v1`, [#48](https://github.com/romulospl/stellytics/issues/48)):** it is served by the portal, on the host `app.stellytics.xyz`. Caddy does not apply a per-IP limit. The existing limit is the portal's, in memory and per instance ([ADR 0006](adr/0006-api-de-leitura-do-scf.md)); a distributed attack is not contained.
- **Domain and DNS in the IaC:** today the records are created by hand (ADR 0004).
- **Production SMTP:** the guide requires a provider, but does not choose or configure one (SPF, DKIM and DMARC for the `mail_from` domain are up to you).
- **Handing the operation over to another team:** plan, order, window, locally tested data migration (`infra/scripts/db-export.sh` and `db-import.sh`) and acceptance criteria in [`docs/transferencia.md`](transferencia.md) ([#54](https://github.com/romulospl/stellytics/issues/54)).
- **High availability:** by default it is a single VM; if it goes down, the service is down until it is recreated (restoring the backup on a new VM, section "Restoring the database"). The "High availability" section above configures the Postgres replica and extra ingestion, but failover is manual, there is no load balancer or floating IP configured and nothing was applied on a real account.
- **Restore test in production:** the restore is tested in CI with a local S3, not against the real bucket. Rehearse it on a disposable VM before you need it.
- **Updating Umami and Postgres:** only through a PR (images by digest in `prod.yaml`); there is no migration guide for a major Postgres version. The Caddy configuration is delivered by Ansible and runs in a container.

## How this guide was checked

Without Hetzner, DNS or SMTP credentials, it was possible to check:

- `docker compose -f infra/compose/prod.yaml config` with a `.env` built from Ansible's `env.j2` (all the required Compose variables come from there);
- `tofu validate` (container `ghcr.io/opentofu/opentofu:1.13.2`) in `infra/opentofu/`;
- `ansible-playbook --syntax-check` of `site.yml`, `monitoring.yml`, `ingest.yml` and `standby.yml` (ansible-core 2.21) with the example files;
- the variable names, OpenTofu outputs, scripts (`pnpm umami:service-account`, `operator:create`) and cited paths, against the repository files;
- the Caddy TLS routes, the internal services, a stored API metric, the encrypted backup and the restore, through the CI `infra` job (`infra/compose/test/prod-stack-test.sh`);
- replication, promotion and replicated ingestion, in local Docker, with `infra/compose/test/ha-replication-test.sh` (`infra` job).

**It was not possible to check** without credentials: the real `tofu apply` and `ansible-playbook`, certificate issuance, email sending, `cosign verify` against real images (there is no published release yet), the visibility of the packages on GHCR and the `backend "s3"` block for the OpenTofu state (`region = "nbg1"` and the `skip_*` settings). The Hetzner console and GitHub steps follow the documentation of those tools and may have changed names in the interface.

**Minimal path.** `init.sh`, `deploy.sh` and `infra/compose/prod-vps.yaml` were added on 19 Nov 2026 and have no recorded run on a real VPS yet. Checking the minimal path, by someone who follows the guide, is also part of the acceptance criterion of [#42](https://github.com/romulospl/stellytics/issues/42).
