# Assay v0.1.0 — Pre-Launch Checklist

This is the manual checklist the maintainer works through to ship Assay v0.1.0
to the public. Everything in the repo is automated up to the point of pushing
a tag; the steps below are the human-in-the-loop bits.

Work top-to-bottom. Don't skip phases.

---

## 1. Pre-flight (one-time setup)

- [ ] Decide on final org name (currently placeholder `chawdamrunal` throughout the repo)
- [ ] Create GitHub org with that name
- [ ] Buy + configure domain (`github.com/chawdamrunal/assay` or final choice)
- [ ] Set up `github.com/chawdamrunal/assay/install` to serve `install.sh` from the repo
      (options: Cloudflare Worker fetching `raw.githubusercontent.com/<org>/assay/main/install.sh`,
      Vercel rewrite, or static hosting with a redirect to the raw URL)
- [ ] Create companion repo `<your-org>/homebrew-tap` (empty — goreleaser will populate it)
- [ ] Decide on final social presence (Twitter/X, Bluesky, Mastodon) — the threat-model
      doc gets posted to them on launch day

---

## 2. Repo prep

- [ ] Search/replace the org placeholder across the tree:
  ```sh
  grep -rl 'chawdamrunal' . \
    --include='*.md' --include='*.yaml' --include='*.yml' \
    --include='*.go' --include='*.json' --include='*.sh' \
    | xargs sed -i '' 's/chawdamrunal/<your-org>/g'
  ```
  Verify nothing important was changed: `git diff`.
- [ ] Run the full local pipeline — must all be green:
  ```sh
  make clean && make build && make test && make lint
  ```
- [ ] Verify the binary works:
  ```sh
  ./bin/assay version
  ./bin/assay auth status
  ```
- [ ] Push to the new GitHub org:
  ```sh
  git remote set-url origin git@github.com:<your-org>/assay.git
  git push -u origin main
  ```

---

## 3. Repo secrets configuration

- [ ] In repo **Settings → Secrets and variables → Actions**, add:
  - `HOMEBREW_TAP_GITHUB_TOKEN` — PAT (classic or fine-grained) with
    `contents: write` on `<your-org>/homebrew-tap`
  - *(no other secrets needed — `GITHUB_TOKEN` is automatic, cosign uses OIDC)*
- [ ] **Settings → Actions → General → Workflow permissions**:
  - Select "Read and write permissions"
  - Tick "Allow GitHub Actions to create and approve pull requests"
- [ ] **Settings → Packages**: confirm `ghcr.io` publishing is enabled for the repo
- [ ] **Settings → Actions → General → Fork pull request workflows**: keep default
      (require approval for first-time contributors)

---

## 4. Verify the release pipeline (dry-run on a tag)

- [ ] Tag a test release:
  ```sh
  git tag v0.0.1-rc1
  git push --tags
  ```
- [ ] Watch the GitHub Actions **release** workflow run end-to-end
- [ ] Verify outputs:
  - Draft release created with binaries + checksums + SBOM
  - Docker image pushed to `ghcr.io/<your-org>/assay:0.0.1-rc1` (multi-arch: amd64 + arm64)
  - Homebrew formula pushed to `<your-org>/homebrew-tap`
  - Cosign signatures attached to the release artifacts
- [ ] If anything fails:
  ```sh
  git tag -d v0.0.1-rc1
  git push --delete origin v0.0.1-rc1
  # delete the draft release in the GitHub UI
  # fix, retry
  ```

---

## 5. Final v0.1.0 release

- [ ] Update `CHANGELOG.md` with any last-minute notes; commit
- [ ] Tag the real release:
  ```sh
  git tag -a v0.1.0 -m "Assay v0.1.0 — initial public release"
  git push --tags
  ```
- [ ] Wait for the release workflow to complete (~10 min)
- [ ] Manually publish the draft release on GitHub Releases
- [ ] Test the one-liner install from a clean machine (or `docker run -it alpine sh`):
  ```sh
  apk add --no-cache curl bash    # alpine only
  curl -sSL https://<your-domain>/install | sh
  assay version
  ```
- [ ] Test `brew install`:
  ```sh
  brew tap <your-org>/tap
  brew install assay
  assay version
  ```

---

## 6. Claude Code marketplace submission

- [ ] Submit `plugin/` directory to the Claude Code plugin marketplace
      (process depends on marketplace state at launch time — check current docs)
- [ ] Add the plugin install instruction to the README:
  ```
  /plugin install <your-org>/assay
  ```

---

## 7. Public launch

- [ ] Post the `docs/threat-model-2026.md` writeup to:
  - Hacker News (title: "Assay: Security scanner for the AI dev stack")
  - Your professional networks (LinkedIn, Bluesky/X, Mastodon)
- [ ] Open a tracking issue: "v0.1.0 shipped — feedback welcome"
- [ ] Update the project status badge in README from "pre-release" to "v0.1.0 shipped"
- [ ] Sleep

---

## 8. Day-after-launch follow-ups

- [ ] Monitor issues + PRs
- [ ] Address any critical bugs with a v0.1.1 patch within 48 hours
- [ ] Engage with HN comments thoughtfully — defenders, not promoters
- [ ] Begin scoping Ring 1 work based on community feedback
- [ ] If something blew up in install/install.sh, hotfix and bump the served version
      (the hosted `/install` endpoint should always point at the latest stable)

---

## Appendix: rollback procedure

If v0.1.0 is broken on public release:

1. Mark the GitHub release as a pre-release (don't delete — people may have cached the URL)
2. Push a v0.1.1 with the fix
3. Update `github.com/chawdamrunal/assay/install` to pin v0.1.1 if needed
4. Note the issue in the next release's CHANGELOG
