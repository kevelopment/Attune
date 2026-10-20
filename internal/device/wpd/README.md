# Windows Portable Devices helper

`wpd-helper.exe` is a small, persistent process. Its main thread initializes COM as an STA, creates the WPD manager, and handles every request and COM call. Standard input and output carry one UTF-8 JSON object per line. Responses have `{ "ok": true, "result": ... }` or `{ "ok": false, "error": "..." }`; transfer progress is a response line with `type`, `done`, and `total`. During a transfer, the helper checks for a `{"op":"cancel"}` line between stream chunks. All remote storage and object IDs are the opaque IDs returned by WPD.

The Go adapter stages transfers in temporary files so the helper protocol does not encode book data as JSON. The helper deletes its temporary download on failure, refuses duplicate sibling names, and deletes only the object it created if an upload fails or is cancelled.

## Build prerequisite

Build releases on Windows with the **Desktop development with C++** workload and Windows SDK installed. The script locates Visual Studio with `vswhere` and initializes its x64 compiler environment when needed. The SDK must provide `PortableDeviceApi.h`, `PortableDevice.h`, and `PortableDeviceGUIDs.lib`. Run `scripts/build-wpd-helper.ps1`; CI and the release script build and package `build/bin/wpd-helper.exe` beside `Attune.exe`. The application expects that exact filename and location at runtime.

## Source-level checks without a device

On Windows, run the build script with MSVC and compile the Go wrapper with `GOOS=windows GOARCH=amd64 go test -c ./internal/device -o $env:TEMP\device.test.exe`. Run `build/bin/wpd-helper.exe` with these lines on standard input to exercise protocol dispatch and the no-device discovery path:

```text
{"op":"unknown"}
{"op":"discover"}
```

The first response must be a structured error, the second a successful JSON array (empty when no WPD device is attached), and the process must exit cleanly at EOF. Hardware transfer behavior still needs validation on a Windows machine with an MTP device.
