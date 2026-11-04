---
name: sync-upstream
description: >
  Sync this fork, younglark/magpie, with upstream yetone/magpie.
  Fetch yetone/main, merge it on a sync branch, keep this fork's own
  behavior where the two sides conflict, and update main only after
  make test passes. The reply ends with what those upstream commits
  changed. Use when the user says "同步上游", "把 yetone 合进来",
  "自动解决冲突", "sync upstream", "sync the fork", or runs /sync-upstream.
---

# Sync upstream

This checkout is the fork `younglark/magpie`. Upstream is `https://github.com/yetone/magpie.git`, branch `main`.

Running this skill authorizes the merge onto a `sync/upstream-*` branch, the push of that branch, and a fast-forward of `main` when the checks below pass. It does not authorize a rebase of `main` or any force-push.

## Rules

- Merge. Do not rebase `main` and do not force-push.
- Before merging, record the fork's own work: `git diff upstream/main...HEAD` and `git log --no-merges upstream/main..HEAD`. That diff and those commit messages are what this fork adds. Preserve that behavior. Take upstream for every other change.
- A conflict in a file outside that diff: take upstream (`git checkout --theirs -- <path>` during the merge) and stage it.
- A conflict in a file inside that diff: keep both behaviors. Start from upstream's side of the file, then port the fork's change from the saved diff and from the non-merge commit messages. Upstream edits in the same file stay. Do not take `--ours` or `--theirs` for the whole file.
- A file only this fork has: keep it. A file only upstream deleted, and that this fork did not change: accept the deletion.
- When the two sides disagree about the same behavior and both cannot stay, stop. Leave the sync branch checked out, push it, and do not update `main`.
- A dirty worktree, or a current branch other than `main`: stop and say so. Do not stash or switch away from someone else's work.
- An `upstream` remote that is not `yetone/magpie`: stop and report the URL. Do not retarget it.

## Steps

1. `git status --porcelain` is empty and `git branch --show-current` is `main`.
2. `git remote get-url upstream` works, or `git remote add upstream https://github.com/yetone/magpie.git`.
3. `git fetch upstream`.
4. `git rev-list --left-right --count HEAD...upstream/main`. The right-hand count is how far behind. If it is 0, say `main` already contains `upstream/main` and that upstream has nothing new, then stop.
5. Save `/tmp/magpie-fork.patch` from `git diff upstream/main...HEAD` and the non-merge log from `git log --oneline --no-merges upstream/main..HEAD`. Save the path list with `git diff --name-only upstream/main...HEAD`.
6. `git switch -c sync/upstream-YYYYMMDD` using today's date. If that branch exists, append `-HHMM`.
7. `git merge upstream/main --no-edit`. A clean merge goes to verification.
8. On conflicts, list `git diff --name-only --diff-filter=U` and resolve each path with the rules above. Finish with no conflict markers (`git diff --check`) and `git add` the resolved paths. Record the upstream tip (`git rev-parse upstream/main`) and the pre-merge `HEAD` before completing the merge. `git commit` with `--no-edit` when `MERGE_HEAD` still exists, so the merge finishes. The message is amended after verification, and only when this run created that commit and it is still unpushed.
9. Verify, in order. Stop at the first failure and do not update `main`. Still finish with the report below.
   - `gofmt -l` on every Go file the resolution edited. Format those files if it prints any, and stage the result.
   - `go test -tags production -count=1` for each module package that contains a fork-patch file or a file that conflicted. The fork's gateway changes are in `internal/gateway`.
   - `make test` before `main` moves. `make test` is `go vet -tags production ./...` and `go test -tags production ./...`.
10. Amend the merge commit so its body names the upstream range, each conflicted path and how it was resolved, and the commands that passed. Include any staged formatting fixes in that amend. Then `git push -u origin HEAD`. Switch to `main`, `git merge --ff-only` the sync branch, and `git push origin main`. If `main` moved and fast-forward fails, merge the sync branch into `main` with a merge commit and push that. Do not push `main` when verification failed or a conflict was left unresolved.
11. Report, in the user's language, and put the upstream changes last:
   - The upstream range (`<pre-merge HEAD>..upstream/main`), the fork commits kept, each conflicted path and the resolution, the test commands and their results, and the `main` SHA. When `main` was not updated, name the sync branch.
   - What upstream changed. Read `git log --no-merges --reverse <pre-merge HEAD>..upstream/main`. For each commit, give its subject and the behavior its body describes. Leave out the test log (lines under `Run`, `Broken by hand`, `Not tried`). When a body does not say what changed, name the areas from `git show --stat --format= <sha>`. After the commits, group them by subsystem (gateway, provider, gui, agent, and the rest): what a user of this fork gained. Do not paste the full diff.

## Schedule

This skill does not install a timer. When the user asks to run it on a schedule, start a durable session schedule that follows these steps. Do not add a GitHub Actions workflow that only runs `gh repo sync`: that stops on conflicts and does not apply the rules above.
