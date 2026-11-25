# Download reliability & control

Status: implemented.

## Scope

1. Engine retries: `Config.MaxRetries` (default 3, 0 = off), per-segment and
   whole-task probe/single-stream retry with exponential backoff (500ms → 10s),
   resume from written bytes, `Task.Retries` counter, `Failed` only after
   exhaustion.
2. Engine global speed limit: `Config.SpeedLimitKBps` (0 = unlimited), one shared
   token bucket for all segments and HLS fragments, live reconfiguration via
   `UpdateConfig`.
3. Settings persistence: `config.json` beside the task state file; IPC
   `GET/POST /config` applies it through `Engine.UpdateConfig`.
4. Desktop UI: four settings fields in the existing modal; retry badge on
   downloading rows.
5. Completion toast: `pkg/notify` shells out to a hidden PowerShell toast with a
   tray-balloon fallback; invoked from `Engine`'s async completion hook.

## Acceptance / verification

- `go vet ./...`, `go build ./...`, `go test -race -count=1 ./...`.
- New engine tests: flaky 5xx/reset server completes with matching SHA-256;
  failure beyond `MaxRetries` ends `Failed`; probe and single-stream retry;
  4xx not retried; resume range starts at written bytes; speed ceiling and
  unlimited; live limit update; pause while throttled; config round-trip.
- Settings modal screenshot under `screenshots/`.

## Deliberately out of scope

- HLS fragment retries (HLS inherits the speed limit but keeps its existing
  fail-fast behavior).
- Notification click actions (the existing row "open folder" button covers it).
