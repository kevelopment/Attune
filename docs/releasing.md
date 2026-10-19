# Building and publishing a release

The release workflow builds on native GitHub-hosted runners, bundles the pinned Kindling helper, and publishes versioned GitHub Releases only after every required build succeeds. Creating this workflow does not publish anything by itself.

The first stable release was `v0.0.1`. Older alpha releases remain available in GitHub Releases.

## Release targets

Release packages include native builds for four platform targets, light and dark macOS icons, a drag-to-Applications DMG, and updater archives. Draft releases are not visible to the in-app updater or public until manually published. Physical-device behavior remains experimental and has not been validated on every platform.

| Runner target | Download | Scope |
| --- | --- | --- |
| macOS, Apple Silicon | `Attune-macos-arm64-0.0.3.dmg` and `Attune-macos-arm64-0.0.3.zip` | DMG installer and updater package |
| macOS, Intel | `Attune-macos-amd64-0.0.3.dmg` and `Attune-macos-amd64-0.0.3.zip` | Same feature scope; separate CPU build |
| Windows, x64 | `Attune-windows-amd64-0.0.3.zip` | Library, preview, updater, experimental USB Kindle management |
| Linux, x64 | `Attune-linux-amd64-0.0.3.tar.gz` | Library, preview, updater, experimental USB Kindle management |

`SHA256SUMS.txt` accompanies all downloadable packages. The macOS DMG contains `Attune.app` and an Applications shortcut; drag the app to Applications to install it. The ZIP remains available for the in-app updater. Windows and Linux packages keep the executable and helper together. Do not move the application executable out of its extracted directory on Windows or Linux.

The macOS release build uses the pinned, build-only Python packages in `scripts/requirements-macos-dmg.txt` to create the Finder layout and animated background. Python is not required to install or run Attune.

Each package includes these files in its `licenses/` directory; the macOS app stores them at `Attune.app/Contents/Resources/licenses/`: `Attune-LICENSE.txt`, `THIRD_PARTY_NOTICES.md`, `THIRD_PARTY_LICENSES.txt`, `Kindling-LICENSE.txt`, and `Kindling-THIRD_PARTY_LICENSES.txt`.

Linux packages are built on Ubuntu 24.04 with GTK3 and WebKit2GTK 4.1. They require compatible runtime libraries on the receiving machine; they are not self-contained AppImages. Windows requires the WebView2 runtime. Platform build prerequisites are described in the [Wails v2 installation guide](https://wails.io/docs/gettingstarted/installation).

## Build artifacts without a release

Run the **Release** workflow manually with the default inputs to build all four platform packages without creating a GitHub Release. These Actions artifacts are labeled `0.0.0-manual` and retained for 14 days. To prepare a draft, set **Create draft** and enter a stable version such as `0.0.3`; the workflow builds all four platform packages, embeds that version, and creates a draft targeting the exact workflow-run commit. Re-running updates the draft only when its version and target commit match. A draft does not appear in the app updater; publish it manually from GitHub Releases when ready. If publishing it creates a tag push, the release workflow preserves the existing release and its assets.

The automated publishing job uses the repository's built-in `GITHUB_TOKEN`, with release write permission confined to that job. No personal access token needs to be committed or configured.

## Publish a version

Release tags use `vMAJOR.MINOR.PATCH` with an optional `-alpha`, `-beta`, or `-rc` suffix, optionally followed by a numeric sequence. For example, `v0.0.1` builds a native app with Wails product version `0.0.1` and publishes a stable GitHub Release. The release scripts embed the full semantic version in the Go binary through linker flags; the updater uses it to compare releases. The frontend package retains the full semantic version too.

To prepare a release before publishing, use the draft workflow described above. Version `v0.0.3` is prepared in this commit. After its release, use the next unused version rather than recreating its tag:

```sh
mise run test
mise run lint
git status --short
git tag -a v0.0.4 -m "Attune v0.0.4"
git push origin v0.0.4
```

Pushing the tag starts the release workflow. It validates the version, builds and packages every target, gathers checksums, and creates the GitHub Release with downloadable archives. Package names and Actions artifact labels include the version. If a required build fails, the publication job does not run. Stable tags omit the prerelease flag.

Do not overwrite an existing release tag to change its contents. Fix the issue and publish the next version instead.

## Signing status

Current macOS artifacts receive an ad-hoc signature so their bundle integrity can be verified locally. They are not signed with a Developer ID or notarized by Apple, so Gatekeeper may block the first launch. Follow [Apple’s Open Anyway steps](https://support.apple.com/en-us/102445) if macOS cannot verify Attune. Windows binaries are not Authenticode-signed. Operating systems may therefore show trust warnings.

Before distributing broadly, provision release signing credentials through GitHub Actions secrets and add the appropriate Developer ID/notarization or Authenticode steps. Certificates, private keys, and passwords must remain outside the repository.

## Conversion helper

Kindling is downloaded at a pinned version and verified against the platform's expected SHA-256 before packaging. Its executable is not tracked in Git. Preserve the included Kindling license and crate notices when redistributing a built app; see [third-party notices](../THIRD_PARTY_NOTICES.md).

When updating Kindling, verify upstream release assets and hashes for every matrix target before updating the pins. A missing asset must fail the build rather than silently shipping an app without conversion support.

## Validation limits

Pull requests run native race tests on macOS arm64, macOS x64, Linux x64, and Windows x64. The Linux lint job checks Go formatting, `go vet`, and TypeScript types. Physical-device behavior has not been validated on every platform.
