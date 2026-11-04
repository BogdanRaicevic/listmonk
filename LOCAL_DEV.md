# Local dev quick start

```sh
./dev/local-start.sh
```

That starts Postgres + MailHog in podman, installs/upgrades the DB schema (idempotent) and runs the app in the foreground (Ctrl+C to stop).

| What        | Where                                                        |
|-------------|--------------------------------------------------------------|
| Admin UI    | http://localhost:9000/admin                                  |
| MailHog UI  | http://localhost:8025 (all outgoing email lands here)        |
| Postgres    | `localhost:5432`, user/pass/db: `listmonk-dev`               |

## Other commands

```sh
podman stop listmonk-db listmonk-mailhog                 # stop containers
podman exec -it listmonk-db psql -U listmonk-dev         # psql shell
make build-frontend                                      # rebuild admin UI after JS/CSS changes
```

## One-time setup notes

- `config.toml` (gitignored) is `dev/config.toml` with db host and app address set to `localhost`.
- The frontend build needs Bun: `npm i -g bun`.
- `go.mod` needs Go >= 1.27.3; the script sets `GOTOOLCHAIN=auto` so Go downloads it automatically.
- SMTP is pointed at MailHog in the DB settings (Settings → SMTP: `localhost:1025`, no auth, no TLS).
  If you ever wipe the `listmonk-dev-db` volume, set that again in the UI.
