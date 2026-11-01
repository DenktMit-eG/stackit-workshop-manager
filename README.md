# STACKIT Workshop Manager

Automates STACKIT workshop account creation and keeps an ad-hoc "password manager"
(username, password, TOTP secret, recovery codes, later the STACKIT project UUID)
for workshop participants. Can also be used to create free STACKIT sandboxes for
personal evaluation.

The tool includes a Go CLI where every registration step is a separate command
and a web UI meant to be used for the workshop participants.

Requirements for getting started: A globally reachable IP with a "free" SMTP port
(or a suitable SMTP redirection pattern) and ideally also a domain. You can get
that easily from a manually created "seed" STACKIT project with a simple VM and
stackit-provisioned domain.

## Architecture

```
cmd/                 cobra CLI (one command per step)
internal/config      config.yaml (catch-all domain, MailHog URL, browser settings)
internal/store       users.yaml user directory (secrets!), step tracking, locked+merged saves
internal/password    pwgen -cny 16 equivalent
internal/mailhog     MailHog v2 API client (search, QP/base64 decoding, wait-for-mail)
internal/totp        RFC 6238 TOTP (SHA1/6 digits/30s) as handed out by the STACKIT IDP
internal/browser     Playwright (headless Chromium) flows against the Zitadel-based STACKIT IDP
internal/steps       step orchestration: created -> registered -> email_verified -> otp_enabled -> project_assigned
internal/web         web UI: Go templates + Datastar (SSE push), background jobs, mail archive
internal/mailstore   local copy of every MailHog mail per user (mail/<slug>/*.json)
internal/jobs        one background job per user (step runs triggered from the UI) with live log
internal/events      in-process event bus (users.yaml changed, job progress, new mail)
```

## Setup

On a fresh Debian/Ubuntu host (needs Go and passwordless sudo):

```sh
scripts/install.sh        # apt: GNU parallel + Chromium shared libs; go build; Playwright driver + Chromium; config.yaml
```

The script runs `browser install --with-deps` (Playwright's own `install --with-deps chromium`,
which knows the package list per Ubuntu release) and falls back to an explicit apt list of the
Chromium libraries (libatk, libnss3, libgbm, …) if that fails. `browser check` launches headless
Chromium once as a smoke test; a failure such as
`error while loading shared libraries: libatk-1.0.so.0` means the libraries are missing.

Manual steps, if you prefer:

```sh
go build -o stackit-ws-manager .
./stackit-ws-manager init                           # writes config.yaml
./stackit-ws-manager browser install --with-deps    # Playwright driver + Chromium + apt libs (~/.cache/ms-playwright*)
./stackit-ws-manager browser check
```

Sizing: every parallel step is one headless Chromium (~300-500 MB RAM). On a 1-CPU / 3 GB VM use
`ACC_JOBS=2 MESH_JOBS=1 scripts/create-accounts.sh …`.

`config.yaml`:

```yaml
catchall_domain: uniphi.de                 # <slug>@<catchall_domain> becomes the STACKIT username
mailhog_url: https://mailhog.mentala.de    # where the mails of the catch-all domain land
users_file: users.yaml                     # the user directory (relative to config.yaml)
portal_url: https://portal.stackit.cloud/
mail:
  wait_timeout: 10m0s                      # STACKIT mails take 1-3 min to show up
  poll_interval: 5s
browser:
  headless: true                           # --headful overrides this for debugging
  slow_mo: 0s
  timeout: 30s
  locale: de-DE                            # the flows match German UI labels
  screenshot_dir: screenshots              # screenshots/html dumps on failure
urls:                                      # how portal links are rebuilt from the stored ids ({id} is replaced)
  stackit_project: https://portal.stackit.cloud/projects/{id}
  meshstack_workspace: https://try.meshstack.io/#/w/{id}/overview
web:
  listen: ":8080"
  password: workshop                       # plain text on purpose; only used when auth_enabled is true
  auth_enabled: false                      # open for now; set true to require the password (cookie session)
  mail_dir: mail                           # local mail archive, synced from MailHog
  mail_sync_interval: 30s
```

## CLI layout

```
stackit-ws-manager
  init                      write config.yaml
  stackit-acc ...           user directory + STACKIT account registration steps
  meshstack ...             meshStack trial workspace, STACKIT project, service account
  serve                     web UI
  mail list|sync            MailHog inspection / local archive
  browser install           Playwright driver + Chromium
```

## Account creation steps (`stackit-acc`)

| command                      | step reached      | what happens                                                                                     |
|------------------------------|-------------------|--------------------------------------------------------------------------------------------------|
| `user-add <slug>`            | `created`         | username `<slug>@<catchall_domain>`, random password (pwgen -cny 16 rules: 16 chars, A-Z, 0-9, symbol) |
| `register <slug>`            | `registered`      | Playwright: portal -> IDP -> "Erstellen Sie ein Konto" -> fill -> "Weiter"                       |
| `verify-email <slug>`        | `email_verified`  | waits in MailHog for "Verifizieren Sie Ihre E-Mail-Adresse" (reports the welcome mail if seen), GETs the verification link and checks for "Email verified successfully" |
| `setup-otp <slug>`           | `otp_enabled`     | Playwright: log in -> MFA prompt -> authenticator app -> read secret -> confirm with computed TOTP -> store recovery codes -> "Weiter"/"Fertig" until the portal dashboard appears |
| `otp <slug>`                 | –                 | prints the current TOTP code (`-q` for the bare code)                                            |
| `onboard <slug>...`          | –                 | runs all remaining registration steps for the given users                                        |

Other commands: `user-list`, `user-show <slug>` (prints password, secret, current OTP, recovery codes,
service account), `user-assign <slug> --name --email` (real participant), `user-set-step <slug> <step>`
(repair), `user-set-project <slug> <uuid>` (manual), `user-remove <slug>`.

Each step checks that the user is at the expected previous step; `--force` overrides that
(e.g. `setup-otp --force` on an already enrolled user just logs in with the stored secret and
verifies it works).

## meshStack steps (`meshstack`)

| command                          | step reached              | what happens                                                                 |
|----------------------------------|---------------------------|------------------------------------------------------------------------------|
| `create-project <slug>`          | `project_assigned`        | opens the trial sign-up (`meshstack.setup_url`), logs in via the STACKIT IDP incl. OTP, fills "Organization name" = slug, waits for "Setting up your workspace" to finish (`setup_timeout`), reads the project UUID from the "Open Console" link |
| `create-service-account <slug>`  | `service_account_created` | logs into `portal.stackit.cloud/projects/<uuid>`, accepts the terms if asked, Rollen -> Service Accounts -> create `ws-…@sa.stackit.cloud` -> key (no expiry) -> downloads the JSON into `meshstack.key_dir` -> Access -> "Zugriff gewähren": grants the account `meshstack.service_account_role` (default `owner`) on the project |
| `grant-role <slug>...`           | –                         | (re-)grants that project role to an existing service account; idempotent                       |
| `onboard <slug>...`              | –                         | runs both                                                                    |
| `sa-key <slug>`                  | –                         | prints the stored key JSON                                                   |

## Terraform landing zone (`terraform`)

| command            | step reached         | what happens                                                                                     |
|--------------------|----------------------|--------------------------------------------------------------------------------------------------|
| `deploy <slug>...` | `terraform_deployed` | copies `terraform.template_dir` to `terraform/<slug>/` (existing directories keep their state), copies the service account key to `keys/sa-key-<id>.json` and extracts `credentials.privateKey` to `keys/sa-key-<id>-private.pem`, writes `teilnehmer.auto.tfvars` from `terraform.tfvars` (`{slug}` expanded), runs `terraform init` + `apply`, writes `output/project.env` (flattened outputs, like the template's distribute.sh) and zips the project to `terraform/<slug>.zip` |
| `zip <slug>`       | –                    | re-creates the zip (without `.terraform`)                                                        |
| `outputs <slug>`   | –                    | prints `output/project.env`                                                                      |
| `destroy <slug>`   | –                    | `terraform destroy`, step falls back to `service_account_created`                                |

The zip (`/users/<slug>/terraform.zip` in the web UI, "download project zip") contains the terraform
code, `terraform.tfstate`, the key files and the outputs, so a participant can `unzip`, `terraform init`
and continue from the deployed state. The full `terraform.log` of every run is kept in the project dir.

```yaml
terraform:
  binary: terraform                 # or tofu
  region: eu01                      # for the API calls (object storage enablement)
  template_dir: terraform-template
  work_dir: terraform               # git-ignored: states and keys live here
  tfvars:
    name_prefix: tn
    name_suffix: g{slug}          # must differ per user: the SCF organization name
    domain: customername-lz{slug}.runs.onstackit.cloud   # (<prefix>lz<suffix>) is unique across all of STACKIT
```

Before `apply`, the Object Storage service is enabled in the project through the STACKIT API
(`terraform enable-s3`, done automatically by `deploy`): the provider's implicit enablement races
when several buckets are created at once (500 / 409 "Two concurrent calls"). A failed apply is
retried once after 30 s because such transient API errors are common.

## Mail archive

`serve` copies every mail addressed to a user from MailHog into `web.mail_dir/<slug>/` (every
`web.mail_sync_interval`), `mail sync` does the same once. Mails are looked up with an exact match
on the full address (`1@example.org`): MailHog's own search is a substring search, so the slug alone
or even the address would also hit `11@example.org`, and the result is paginated, so the client pages
through all matches. Files are named `<first seen>-<hash of the MailHog id>.json`; a mail is
archived once even when the sink reports a changing `Created` time (the MailHog maildir backend
does). `mail dedupe` (also run on `serve` start) removes duplicates and stale `.json.tmp` files left
by older versions or a full disk.

Note for the MailHog host: with `MH_STORAGE=maildir` MailHog prints every mail body to stdout on
each API read. Together with the periodic sync this floods the journal/syslog (observed: ~20k lines
per minute, disk full within a day). Silence it with a systemd drop-in
(`StandardOutput=null`, `StandardError=null` in `/etc/systemd/system/mailhog.service.d/quiet.conf`).

## Jumphost

The detail page shows the jumphost of a deployed landing zone: host name, public IP (from the
`jumphost_ip` output, else from the terraform state), ssh user/password (`ubuntu`, the terraform
`jumphost_password`), and the web shell link `https://shell:<password>@<host>/` (GoTTY behind Caddy,
basic-auth user `shell`). The zip of the terraform project (code, state, service account key) is
offered on the same page.

Each jumphost also carries its own deployment: the zip is unpacked to `/home/ubuntu/landing-zone`
(code, state, keys, `terraform init` done), so participants can continue with `terraform plan`
there. The extra tools from `modules/jumphost/cloud-init/install-manual.sh` (kubectl, terraform,
helm, uv, stackit CLI, cf, pack, argocd, edit, k9s) are installed to `/usr/local/bin`, the
forgejo runner from `install-forgejo-runner.sh` too, and `adhoc-wireguard.sh` set up the wireguard
server (`wg0`, UDP 51820) with the client config at `/home/ubuntu/jumphost.conf`. All of this was
pushed by hand for the first ten hosts (2026-10-21), because the cloud-init shell-script parts
referenced `cloud-init/` relative to the working directory and were silently dropped; the module now
uses `path.module`. Note that any change to the cloud-init data replaces the jumphost on the next
`terraform apply`.

## Batch creation

`scripts/create-accounts.sh [prefix] [count]` (GNU parallel required) creates `<prefix>1..<count>`:
STACKIT registration via `stackit-acc onboard --create`, `ACC_JOBS` (default 5) accounts at a time,
then `meshstack onboard` (`MESH_JOBS`, default 3) and `terraform deploy` (`TF_JOBS`, default 3). Every step is idempotent (finished steps
are skipped), so simply rerun the script if some accounts failed.

```sh
scripts/create-accounts.sh customername 20
./stackit-ws-manager stackit-acc user-list
./stackit-ws-manager stackit-acc otp customer1
./stackit-ws-manager meshstack sa-key customer1 > customer1-key.json
```

`stackit-acc verify-email --resend` logs in with the unverified account and clicks "Code erneut senden" when the
first mail never shows up (happened once in five test runs).

## Web UI

```sh
./stackit-ws-manager serve                # http://localhost:8080 (config web.listen, or --listen)
scripts/install-service.sh                # or: run it as a systemd user service (see below)
```

`scripts/install-service.sh` installs `serve` as a systemd *user* service (`stackit-ws-manager.service`,
linger enabled so it survives logouts) plus a path unit that watches the binary and restarts the
service whenever it changes, so a plain `go build -o stackit-ws-manager .` deploys. Logs:
`journalctl --user -u stackit-ws-manager -f`; remove with `scripts/install-service.sh remove`.

* Overview `/`: one row per user with username / password (masked, reveal) / OTP (always shown,
  refreshed every second with countdown via `/otps` SSE) in read-only input fields that select their
  value on click, plus copy-to-clipboard buttons, the account state and a "details" button. All
  actions live on the detail page. "Add user" at the top. The table is pushed from the server over
  SSE (`/events`) whenever users.yaml changes (also when the CLI changes it), a job logs a line, or
  new mail arrives – no reload needed.
* Detail `/users/<slug>`: all fields incl. OTP secret, recovery codes, meshStack workspace link,
  service account e-mail and key (download `/users/<slug>/sa-key.json`, shown inline), participant
  and project editing, one button per registration / meshStack step (+ "test login") with the live job
  log, the archived e-mails of the user (`/users/<slug>/mail/<id>`, HTML rendered in a sandboxed
  iframe), and all screenshots/HTML dumps of that user.
* Mail archive: every mail addressed to a known user is copied from MailHog into `mail/<slug>/`
  (every `mail_sync_interval`, on "sync", or via `mail sync` on the CLI) so the history survives
  MailHog purges and can be used for password recovery later.
* Auth: `web.auth_enabled: true` puts a login form in front of everything (single shared password
  from `web.password`, cookie session, Datastar requests get 401 instead of a redirect).
* Tech: plain HTML, Go `html/template`, Datastar v1.0.4 (vendored in `internal/web/static`).
  Datastar v1 attribute syntax uses colons for keys: `data-on:click`, `data-bind:slug`,
  `data-signals:foo__ifmissing`. Note that `html/template` treats `data-on:*` as a JavaScript
  context (it strips the `data-` prefix), so string values are emitted as quoted JS literals.

## Behaviour of meshStack / the STACKIT portal worth knowing

* The trial sign-up shows "One more thing" (Organization name) on setup.try.meshstack.io *before*
  redirecting to try.meshstack.io; "Setting up your workspace" (terraform) took ~100 s in tests.
* The workspace overview shows a cookie banner and a "Welcome to your meshStack trial" chooser
  (Platform Team / Application Team) on every visit until a choice is made; the flow declines cookies
  and picks "Application Team".
* The organization name is the slug; meshStack rejects very short names, so slugs shorter than 3
  characters or not starting with a letter get `meshstack.org_name_prefix` (default `project`)
  prepended, e.g. slug `1` -> organization `project1`.
* The STACKIT project UUID comes from the href of the "Open Console" link on the overview.
* With many simultaneous trial sign-ups (18 at once) meshStack shows "Something went wrong – we're
  still setting up part of your workspace". Continuing there leaves a workspace *without* a project
  (that is how asvic5 ended up), so `create-project` does not continue but retries the whole sign-up
  later (4 attempts, 60 s, 120 s, …). Keep meshStack parallelism low (`MESH_JOBS`, default 3).
* A workspace that exists without a project is not handled automatically; `meshstack create-project`
  fails with "'Open Console' link not found" in that case.
* A freshly created service account has **no role** on its project (it does not even appear on the
  Access page). Terraform then fails at the first permission-checked call, e.g.
  `stackit_observability_instance`: `Loading service plan ... 403 Forbidden`, while resources whose
  plan validation does not call the API look fine. Hence the role grant in `create-service-account`.
* Right after project creation the portal may answer 403 (`iam.service-account-key.list`) on the
  Service Account Keys page; the flow reloads every 10 s for up to 2 min. An existing `ws-…` service
  account is reused instead of creating a second one, and its e-mail is stored even if the key step fails.
* Playwright URL globs: a single `*` does not match `/`, use `**` for paths (`https://try.meshstack.io/**`).

## Behaviour of the STACKIT IDP worth knowing

* Password policy: min. 16 characters, upper + lower case, digit, symbol.
* Server-side errors (e.g. "Dieser Benutzer ist bereits registriert.") are shown as toasts
  (`.lgn-toast.toast-error`), field errors inside `.lgn-error` (but `.lgn-error.info-msg` is a hint).
* Many sign-ups from one IP in a short time get HTTP 429 on the sign-up POST (seen after ~15
  registrations with 5 in parallel); `register` retries up to 4 times with growing pauses (90 s, 180 s, …).
* Under parallel load the "auch von Hand in" click occasionally does not open the secret modal;
  `setup-otp` clicks once more.
* The sign-up submit click is sometimes swallowed by the password-policy dropdown fade-out;
  `register` waits for the actual POST to `/ui/login/register` and re-clicks if none was sent.
* After sign-up the IDP shows "Überprüfen Sie Ihren Email-Posteingang" (code entry). We do not
  use the code; calling the link from the mail verifies the address just as well.
* MailHog receives the welcome mail ~1-3 minutes after sign-up and the verification mail ~1 minute later.
* The welcome mail is not sent reliably; only the verification mail is awaited.
* After confirming the first TOTP code the IDP shows recovery codes (`/mfa/init/recovery_codes`,
  10 codes, also in `#lgn-download-button[data-codes]`) and a done page (`/mfa/init/done`, "Fertig").
* On later logins the IDP asks for a code on `/mfa/verify` ("Verifizieren" button).

## Files

* `users.yaml` – contains passwords and TOTP secrets. Mode 0600, git-ignored. Per user it stores the
  STACKIT project as `stackit_project: {id, service_account: {email, key_file}}` and the meshStack
  workspace as its bare id (`meshstack_workspace`); the portal URLs are rebuilt from the `urls`
  templates in config.yaml (shown on the user detail page and by `user-show`). Records written by
  earlier versions (`project_id`, `service_account`, workspace URL) are migrated on load. Saves are
  flock-protected and merged, so several CLI runs and the web UI may work in parallel.
* `screenshots/` – full-page screenshots `<slug>-<step>-<timestamp>.png` on every failure (and after
  `register`/`setup-otp` success).
* `mail/<slug>/` – archived MailHog messages (raw JSON).
* `keys/` – downloaded service account key JSON files (mode 0600, git-ignored).

## Container

```sh
docker build -t stackit-ws-manager .
docker run --rm -v $PWD/data:/data stackit-ws-manager user list          # /data holds config.yaml, users.yaml, mail/, screenshots/
docker run --rm -p 8080:8080 -v $PWD/data:/data stackit-ws-manager serve
```
