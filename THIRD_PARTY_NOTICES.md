# Third-party notices

Attune uses the Kindling command-line converter, a separate third-party project by Francisco Riordan. The local build fetches Kindling v0.45.6 from its upstream GitHub release and verifies the pinned SHA-256 before packaging it. The executable is excluded from this repository; its upstream MIT license is retained at [`third_party/kindling/LICENSE`](third_party/kindling/LICENSE), with source and release details in [`third_party/kindling/README.md`](third_party/kindling/README.md).

Go and frontend dependency versions are recorded in `go.mod`/`go.sum` and `frontend/package-lock.json`. Their upstream license files remain with the corresponding packages.
