# Building and publishing a release

The release workflow builds on native GitHub-hosted runners, bundles the pinned Kindling helper, and publishes versioned GitHub Releases only after every required build succeeds. Creating this workflow does not publish anything by itself.

## Release targets

| Runner target | Download | Scope |
| --- | --- | --- |
| macOS, Apple Silicon | `Attune-macos-arm64.zip` | Library, preview, and USB Kindle management |
| macOS, Intel | `Attune-macos-amd64.zip` | Same feature scope; separate CPU build |
| Windows, x64 | `Attune-windows-amd64.zip` | Experimental library and preview; USB management unavailable |
| Linux, x64 | `Attune-linux-amd64.tar.gz` | Experimental library and preview; USB management unavailable |

`SHA256SUMS.txt` accompanies the downloadable archives. macOS packages contain `Attune.app`; Windows and Linux packages keep the executable and helper together. Do not move the application executable out of its extracted directory on Windows or Linux.

Linux packages are built on Ubuntu 24.04 with GTK3 and WebKit2GTK 4.1. They require compatible runtime libraries on the receiving machine; they are not self-contained AppImages. Windows requires the WebView2 runtime. Platform build prerequisites are described in the [Wails v2 installation guide](https://wails.io/docs/gettingstarted/installation).

## First GitHub setup

When you are ready to put the project on GitHub:

1. Create an empty repository and add it as this checkout's `origin` remote.
2. Push `main` and enable GitHub Actions for the repository.
3. Run the release workflow manually first. Manual runs build downloadable Actions artifacts without publishing a release.
4. Inspect those archives and test each platform before tagging a public version.

The automated publishing job uses the repository's built-in `GITHUB_TOKEN`, with release write permission confined to that job. No personal access token needs to be committed or configured.

## Publish a version

Release tags use `vMAJOR.MINOR.PATCH`, for example `v0.1.0`. Check the local tree and tests before creating a tag:

```sh
./scripts/test.sh
git status --short
git tag -a v0.1.0 -m "Attune v0.1.0"
git push origin v0.1.0
```

Pushing the tag starts the release workflow. It validates the version, builds and packages every target, gathers checksums, and creates the GitHub Release with downloadable archives. If a required build fails, the publication job does not run. Manual workflow dispatch remains build-only and embeds version `0.0.0`.

Do not overwrite an existing release tag to change its contents. Fix the issue and publish the next version instead.

## Signing status

Current macOS artifacts receive an ad-hoc signature so their bundle integrity can be verified locally. They are not signed with a Developer ID or notarized by Apple. Windows binaries are not Authenticode-signed. Operating systems may therefore show trust warnings.

Before distributing broadly, provision release signing credentials through GitHub Actions secrets and add the appropriate Developer ID/notarization or Authenticode steps. Certificates, private keys, and passwords must remain outside the repository.

## Conversion helper

Kindling is downloaded at a pinned version and verified against the platform's expected SHA-256 before packaging. Its executable is not tracked in Git. Preserve its license when redistributing a built app; see [third-party notices](../THIRD_PARTY_NOTICES.md).

When updating Kindling, verify upstream release assets and hashes for every matrix target before updating the pins. A missing asset must fail the build rather than silently shipping an app without conversion support.

## Validation limits

Local macOS builds and Go race/vet checks can be run in this checkout. Linux and Windows source compile checks do not substitute for launching their GUI builds on those operating systems. The first GitHub Actions run is still needed to validate the hosted runner configuration; hardware testing is still needed for device support.
