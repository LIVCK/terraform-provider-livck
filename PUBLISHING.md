# Publishing to the Terraform Registry

One-time setup (~15 minutes), then every release is a signed git tag.

## 0. Decisions still open

- [ ] **License**: `LICENSE` currently carries an MPL-2.0 stub header. Decide
      MPL-2.0 (HashiCorp ecosystem default) vs. MIT and commit the full text.
- [ ] **GitHub repo**: must be public and named exactly `terraform-provider-livck`
      (the registry namespace comes from the GitHub org → `livck/livck` as the
      provider source requires the org to be `livck`, or adjust `main.go`'s
      address + docs to the actual namespace, e.g. `LIVCK/livck`).

## 1. One-time: GPG key (RSA — the registry does NOT accept ECC)

```sh
gpg --full-generate-key        # choose RSA and RSA, 4096 bit
gpg --armor --export "<KEY_ID>" > livck-registry.pub.asc
gpg --armor --export-secret-keys "<KEY_ID>"   # → GitHub secret GPG_PRIVATE_KEY
```

- Add the **public** key in the Registry: https://registry.terraform.io →
  Settings → GPG Keys (sign in with the GitHub org).
- Add GitHub Actions secrets on the provider repo: `GPG_PRIVATE_KEY`,
  `PASSPHRASE`.

## 2. One-time: connect the repo

Registry → *Publish → Provider* → select the GitHub org + the
`terraform-provider-livck` repo. A webhook ingests every future release.

## 3. Every release

```sh
git tag v0.1.0 && git push origin v0.1.0
```

The `release.yml` workflow runs GoReleaser: per-OS/arch zips,
`…_SHA256SUMS`, the binary `…_SHA256SUMS.sig` (GPG-signed) and the
`…_manifest.json` (protocol 6). The registry picks the release up
automatically.

## 4. OpenTofu registry (optional, separate)

OpenTofu uses the same releases but its own registry — submit the provider
once via PR to https://github.com/opentofu/registry (reuses the GPG-signed
GitHub releases).
