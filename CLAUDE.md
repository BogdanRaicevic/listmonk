# Fork workflow

This repository is Bogdan Raicevic's fork of [listmonk](https://github.com/knadh/listmonk). Ongoing development and personal releases live on `personal`. `master` stays a clean copy of the original project so upstream pull requests stay easy to cut.

`AGENTS.md` repeats this file. Update both when these rules change.

## Remotes

| Remote | Repository | Role |
|---|---|---|
| `origin` | `https://github.com/BogdanRaicevic/listmonk.git` | this fork; the only remote to push to |
| `upstream` | `https://github.com/knadh/listmonk.git` | the original project; fetch from it |

Never push to `upstream`. The fork does not have write access there.

## Branches

- `master` matches `upstream/master`. It is only a mirror. Do not commit features, fixes, local-dev files, or these agent docs on `master`.
- `personal` is the long-lived branch for this fork. Develop here, merge finished work here, tag personal releases here, and push here.
- Short-lived topic branches (`feature/…`, `fix/…`) start from `personal` and merge back into `personal`.
- A branch meant for the original project starts from an up-to-date `master`, never from `personal`.

`personal` already carries fork-only work: campaign exclude lists, the analytics open/click rate fix, and the local-dev helper (`LOCAL_DEV.md`, `dev/local-start.sh`).

## Day-to-day development

```sh
git checkout personal
git pull origin personal
git checkout -b feature/short-name
# edit, test, commit
git push -u origin HEAD
```

When the topic branch is ready, merge it into `personal` and push:

```sh
git checkout personal
git merge feature/short-name
git push origin personal
```

`personal` is a published branch. Update it with merge. Do not rebase it and do not force-push it.

## Bring in the owner's new commits

Do this when upstream has moved and the fork should pick up those changes. Conflicts are resolved on `personal` only.

```sh
git fetch upstream
git checkout master
git merge --ff-only upstream/master
git push origin master
git checkout personal
git merge master
git push origin personal
```

If `git merge --ff-only` refuses, `master` is no longer a clean mirror. Stop and fix that before merging anything else. Do not merge `personal` into `master`.

## Pull requests to the original project

Open these against `knadh/listmonk` `master` from time to time. Each PR contains one change the upstream maintainers should consider.

1. Update the mirror: `git fetch upstream && git checkout master && git merge --ff-only upstream/master`.
2. Branch from that `master`: `git checkout -b fix/upstream-short-name`.
3. Cherry-pick or reimplement only the commits that belong upstream. Leave fork-only files out of the branch:
   - `AGENTS.md`
   - `CLAUDE.md`
   - `LOCAL_DEV.md`
   - `dev/local-start.sh`
4. Push the branch to this fork: `git push -u origin HEAD`.
5. Open the pull request with base `knadh/listmonk:master` and compare `BogdanRaicevic/listmonk:<branch>`.

Follow `CONTRIBUTING.md`. Upstream asks for a proposal issue before a PR, wants small single-purpose PRs, and squashes on merge. Until listmonk v7.0.0, maintainers are not accepting pull requests that change the admin frontend/UI.

After the upstream PR is open, keep developing on `personal`. Do not retarget that work onto `master`.

## Local dev

`./dev/local-start.sh` starts Postgres and MailHog and runs the app. Details are in `LOCAL_DEV.md`. `config.toml` is gitignored; do not commit it or any secrets.
