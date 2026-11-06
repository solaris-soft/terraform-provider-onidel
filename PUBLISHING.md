# Publishing to the Terraform Registry

This is a community provider under `solaris-soft/onidel`, not an official Onidel or HashiCorp provider. Repository configuration alone does **not** publish or certify it. An authorized maintainer must complete the GitHub and HCP Terraform steps below.

## Repository readiness

- The GitHub repository must be **public** and named `terraform-provider-onidel` (lowercase). The configured origin is `solaris-soft/terraform-provider-onidel`.
- `main.go` serves `registry.terraform.io/solaris-soft/onidel` using Terraform Plugin Framework's default **protocol 6**. `terraform-registry-manifest.json` declares `6.0`.
- `docs/index.md`, `docs/resources/virtual_machine.md`, and `docs/data-sources/virtual_machine.md` cover the provider and all registered resource/data-source types. Documentation is versioned with each release tag. Preview it with the [Registry doc preview tool](https://registry.terraform.io/tools/doc-preview).
- The MIT license is in `LICENSE` and included in release archives. Keep credentials, state, plan files, and signing keys out of Git.
- `.goreleaser.yml` creates pure-Go ZIPs for macOS, Linux, FreeBSD, and Windows, injects the version into the binary, includes the Registry manifest in checksums, and signs the checksums with GPG.
- `.github/workflows/release.yml` runs the test workflow before accessing signing secrets or publishing. It accepts `vMAJOR.MINOR.PATCH` tags and optional SemVer prereleases, not build metadata.

Before tagging, run the checks in `.github/workflows/test.yml` (including `govulncheck` and the mock Terraform smoke test) and review the plan/replacement and API limitations in `README.md`. CI and releases use **Go 1.27.3**; keep both workflow versions patched and synchronized. Updated networking/gRPC dependencies require Go 1.26 or later. The tests do not provision real cloud infrastructure; Registry compliance does not establish production API compatibility.

For a local packaging rehearsal, install Go 1.27.3 and **GoReleaser v2.20.0**, then run:

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=sign,publish
```

Snapshot artifacts under `dist/` are for testing only, not Registry publication. Inspect ZIP contents and checksums. This rehearsal does not validate the real signing key or GitHub permissions.

## One-time account and signing setup (manual)

1. In a non-Europe HCP Terraform organization, [create or claim the `solaris-soft` public namespace](https://developer.hashicorp.com/terraform/cloud-docs/users-teams-organizations/organizations/public-namespace). Ensure the publisher is an organization owner or has **Manage public providers** permission. GitHub organization/repository access is also required.
2. Make the repository public and allow GitHub Actions and the actions used in both workflows. Install the **Terraform Cloud GitHub App** for this repository if it is not already installed globally. Protect the default branch and restrict who can create `v*` release tags: release workflows execute repository code with a signing key and a write-capable token.
3. Generate a dedicated, passphrase-protected **RSA** GPG signing key. HashiCorp currently accepts RSA/DSA keys, **not the default ECC key type**. For example, on a trusted maintainer machine:

   ```sh
   gpg --full-generate-key
   # Select RSA and RSA (not ECC), use at least 3072 bits, and set a strong passphrase.
   gpg --list-secret-keys --keyid-format LONG
   gpg --armor --export 'KEY_FINGERPRINT'
   gpg --armor --export-secret-keys 'KEY_FINGERPRINT'
   ```

   Never commit the secret key or paste it into issues/logs. Store a secure backup and track its expiry. Export the public and private keys separately.
4. Add the **public** ASCII-armored key to the Terraform Registry's **User Settings > Signing Keys** for the `solaris-soft` namespace. Check the fingerprint matches your release key. For key rotation, register the new public key before releasing with it.
5. Add GitHub Actions repository secrets:
   - `GPG_PRIVATE_KEY`: the ASCII-armored **private** key.
   - `PASSPHRASE`: its non-empty passphrase.

   `GITHUB_TOKEN` is supplied automatically; the release job requests `contents: write`. Do not add private keys to Terraform configuration or this repository.

## First release and namespace publication

1. Merge the release configuration and wait for CI to pass. Review the exact commit to be released. Ensure no branch has the same name as the planned tag.
2. Create and push a new version tag (choose the version deliberately):

   ```sh
   git tag v0.1.0
   git push origin v0.1.0
   ```

3. Wait for **Release** to finish. Check that the GitHub Release is finalized (not a draft) and contains:
   - `terraform-provider-onidel_VERSION_OS_ARCH.zip`, containing `terraform-provider-onidel_vVERSION` (`.exe` on Windows).
   - `terraform-provider-onidel_VERSION_manifest.json` with protocol `6.0`.
   - `terraform-provider-onidel_VERSION_SHA256SUMS`, covering every ZIP **and the manifest**.
   - `terraform-provider-onidel_VERSION_SHA256SUMS.sig`, a **binary detached GPG signature**, not an ASCII-armored signature.

   In a fresh directory containing the downloaded release assets, verify with the registered public key:

   ```sh
   gpg --verify terraform-provider-onidel_VERSION_SHA256SUMS.sig terraform-provider-onidel_VERSION_SHA256SUMS
   sha256sum --check terraform-provider-onidel_VERSION_SHA256SUMS
   ```

4. In HCP Terraform, go to **Organization > Registry > Public namespaces > solaris-soft > Publish > Provider**. Select the repository, choose an appropriate category, review/accept the terms, and click **Publish provider**.
5. Check version processing status and resolve any errors. In a clean directory **without a development override**, run `terraform init` with:

   ```hcl
   terraform {
     required_providers {
       onidel = {
         source  = "solaris-soft/onidel"
         version = "0.1.0"
       }
     }
   }
   ```

   Use the actual released version. Confirm Terraform verifies the expected signing key, then run `terraform providers schema -json`. Installation and schema inspection need no Onidel API credentials and do not create VMs. Review the rendered Registry docs too.

## Subsequent releases and recovery

Push a **new** SemVer tag after tests pass. GitHub release events notify the Registry, which ingests each version; check its status in the public namespace. Prereleases are available only when explicitly selected and are not automatically chosen as the latest stable version.

**Never move a published tag or replace published assets**: users' lock-file checksums would no longer match. Release fixes under a new version. This also applies to documentation changes. If an unpublished workflow fails, inspect whether any release/assets became public before retrying; use a new version whenever there is doubt. Do not enable artifact replacement to force a retry.

If a release is missing from the Registry, inspect the reported processing error, signing key, manifest, assets, GitHub App access, and release webhook. Use the namespace artifact's **Resync** action after resolving the cause. Creating a new version is recommended for errors in an already-published version.

## Authoritative references

- [Publish namespace artifacts](https://developer.hashicorp.com/terraform/cloud-docs/users-teams-organizations/organizations/public-namespace/artifacts#publish-artifacts)
- [Provider publishing requirements](https://developer.hashicorp.com/terraform/registry/providers/publishing)
- [Provider documentation format](https://developer.hashicorp.com/terraform/registry/providers/docs)
- [Registry terms](https://registry.terraform.io/terms)
