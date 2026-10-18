# Building and publishing a release

The release workflow builds on native GitHub-hosted runners, bundles the pinned Kindling helper, and publishes versioned GitHub Releases only after every required build succeeds. Creating this workflow does not publish anything by itself.

The first release is `v0.0.1-alpha.1`, an early preview. GitHub marks alpha, beta, and release-candidate tags as prereleases.

## Release targets

These are the currently published `v0.0.1-alpha.1` packages. Its Windows and Linux builds predate the experimental USB support now in the source tree.

| Runner target | Download | Scope |
| --- | --- | --- |
| macOS, Apple Silicon | `Attune-macos-arm64-0.0.1-alpha.1.zip` | Library, preview, and USB Kindle management |
| macOS, Intel | `Attune-macos-amd64-0.0.1-alpha.1.zip` | Same feature scope; separate CPU build |
| Windows, x64 | `Attune-windows-amd64-0.0.1-alpha.1.zip` | Experimental library and preview; USB management unavailable |
| Linux, x64 | `Attune-linux-amd64-0.0.1-alpha.1.tar.gz` | Experimental library and preview; USB management unavailable |

`SHA256SUMS.txt` accompanies the downloadable archives. macOS packages contain `Attune.app`; Windows and Linux packages keep the executable and helper together. Do not move the application executable out of its extracted directory on Windows or Linux.

Each package includes these files in its `licenses/` directory; the macOS app stores them at `Attune.app/Contents/Resources/licenses/`: `Attune-LICENSE.txt`, `THIRD_PARTY_NOTICES.md`, `THIRD_PARTY_LICENSES.txt`, `Kindling-LICENSE.txt`, and `Kindling-THIRD_PARTY_LICENSES.txt`.

Linux packages are built on Ubuntu 24.04 with GTK3 and WebKit2GTK 4.1. They require compatible runtime libraries on the receiving machine; they are not self-contained AppImages. Windows requires the WebView2 runtime. Platform build prerequisites are described in the [Wails v2 installation guide](https://wails.io/docs/gettingstarted/installation).

## Build artifacts without a release

Run the **Release** workflow manually to build all four platform packages without publishing a GitHub Release. These Actions artifacts are labeled `0.0.0-manual` and retained for 14 days.

The automated publishing job uses the repository's built-in `GITHUB_TOKEN`, with release write permission confined to that job. No personal access token needs to be committed or configured.

## Publish a version

Release tags use `vMAJOR.MINOR.PATCH` with an optional `-alpha`, `-beta`, or `-rc` suffix, optionally followed by a numeric sequence. For example, `v0.0.1-alpha.1` builds a native app with product version `0.0.1` and publishes a GitHub prerelease. The frontend package retains the full semantic version `0.0.1-alpha.1`.

Check the local tree and checks before creating a tag:

```sh
mise run test
mise run lint
git status --short
git tag -a v0.0.1-alpha.1 -m "Attune v0.0.1-alpha.1"
git push origin v0.0.1-alpha.1
```

Pushing the tag starts the release workflow. It validates the version, builds and packages every target, gathers checksums, and creates the GitHub Release with downloadable archives. Package names and Actions artifact labels include the version. If a required build fails, the publication job does not run. Stable tags omit the prerelease flag.

Do not overwrite an existing release tag to change its contents. Fix the issue and publish the next version instead.

## Signing status

Current macOS artifacts receive an ad-hoc signature so their bundle integrity can be verified locally. They are not signed with a Developer ID or notarized by Apple. Windows binaries are not Authenticode-signed. Operating systems may therefore show trust warnings.

Before distributing broadly, provision release signing credentials through GitHub Actions secrets and add the appropriate Developer ID/notarization or Authenticode steps. Certificates, private keys, and passwords must remain outside the repository.

## Conversion helper

Kindling is downloaded at a pinned version and verified against the platform's expected SHA-256 before packaging. Its executable is not tracked in Git. Preserve the included Kindling license and crate notices when redistributing a built app; see [third-party notices](../THIRD_PARTY_NOTICES.md).

When updating Kindling, verify upstream release assets and hashes for every matrix target before updating the pins. A missing asset must fail the build rather than silently shipping an app without conversion support.

## Validation limits

Pull requests run native race tests and lint checks on macOS arm64, macOS x64, Linux x64, and Windows x64. Hardware testing is still needed for device support.
