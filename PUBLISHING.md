# Publishing to the Terraform Registry

The registry does not host code. It watches GitHub releases, so publishing comes
down to a signed tag. Setting it up takes about fifteen minutes once.

## Before you start

Two things to settle:

**The repository** must be public and named exactly `terraform-provider-livck`.
The registry derives the namespace from the GitHub organization, which is what
makes the source `livck/livck`. If the org is named differently, adjust the
address in `main.go` and the docs to match.

**The license.** `LICENSE` currently holds MPL-2.0, the usual choice in the
HashiCorp ecosystem. Swap it for MIT if you would rather.

## One time: the signing key

The registry verifies release signatures and only accepts RSA or DSA, not ECC.

```sh
gpg --full-generate-key          # pick RSA and RSA, 4096 bit
gpg --armor --export "<KEY_ID>" > livck-registry.pub.asc
gpg --armor --export-secret-keys "<KEY_ID>"
```

Add the public key at registry.terraform.io under Settings > GPG Keys, signed
in with the GitHub organization. Then add two secrets to the provider repo:
`GPG_PRIVATE_KEY` (the exported private key) and `PASSPHRASE`.

## One time: connect the repo

In the registry, go to Publish > Provider and pick the organization and the
`terraform-provider-livck` repository. That installs a webhook, and every
release from then on is picked up on its own.

## Every release

```sh
git tag v0.1.0
git push origin v0.1.0
```

The release workflow runs GoReleaser, which builds the per-platform archives,
the `SHA256SUMS` file, its GPG signature, and the protocol manifest. The
registry takes it from there.

## OpenTofu

OpenTofu reads the same GitHub releases but keeps its own registry. Submitting
the provider is a one-off pull request against `github.com/opentofu/registry`;
nothing about the release process changes.
