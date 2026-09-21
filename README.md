# go-logger

Structured JSON logging with hierarchical categories and live configuration, built on zap.
Requires Go 1.24 or later.

```go
log, err := logger.New(logger.Config{})
if err != nil {
    return err
}
consumer := log.Category("rabbitmq").Category("consumer")
consumer.Info("message received", map[string]any{"queue": "events"})
if err := log.SetCategoryLevel("rabbitmq", logger.DebugLevel); err != nil {
    return err
}
consumer.Debug("consumer ready") // Existing instances see updates.
return log.Sync()
```

Defaults: info level, UTC, `2006-01-02 15:04:05`, stdout, and category
`application` for an unnamed logger. `Category("http")` names the category
`http`, without an implicit `application` prefix. Category names use dot-separated,
nonempty components. Configuration and runtime rule updates validate names;
`Category`/`Named` follow zap naming semantics, so callers should use valid names.

Every line contains `date`, `level`, `category`, `msg`, `extra`, in that order.
Additional fields, including `With` fields, appear inside `extra`; it is always
an object. Duplicate field names use the last serialized value. Date formats
use Go layouts. Configuration has JSON/YAML tags; loading application files is
left to the application.

The logger does not require or open a configuration file. If the application's
optional config file is absent, pass `Config{}` to `New`: every category uses
`info` unless `LOG_LEVEL` is set, so by default debug entries are filtered and
info/warn/error entries are written.
The application should handle a missing file before calling New; other file
errors and invalid configuration should still be reported.

Level and enabled overrides inherit independently from the nearest ancestor.
A child with `Enabled: true` can re-enable a disabled subtree. ResetCategory
removes both local overrides, leaving descendants' explicit rules intact.
An update publishes a complete immutable snapshot. Entries already accepted by
Check may finish writing after an update.

## Environment configuration

Set `LOG_LEVEL=debug` (or info, warn, error, dpanic, panic, fatal) to override
the default level without a configuration file. Priority: nonempty `LOG_LEVEL`,
then `Config.DefaultLevel`, then `info`. Individual category overrides retain
their priority. Values are case-insensitive and surrounding whitespace is ignored.
Empty or unset values leave the configuration unchanged; invalid nonempty values
return a constructor error identifying `LOG_LEVEL`.

The variable is read once when New creates a logger. Subsequent changes use
SetDefaultLevel; existing loggers do not reread the environment.

## Runtime configuration

All methods below are safe to call concurrently. Changes apply to existing
loggers derived through Category, Named, or With; separate calls to New have
independent configuration. Category arguments are absolute: calling
`log.Category("http").SetCategoryLevel("database", logger.WarnLevel)` updates
`database`, not `http.database`.

| Method | Effect |
| --- | --- |
| `Resolve(category)` | Returns the inherited level and enabled state. |
| `SetDefaultLevel(level)` | Replaces the fallback, including an initial LOG_LEVEL value; explicit category rules remain. |
| `SetCategoryLevel(category, level)` | Sets a level override, preserving the enabled setting. |
| `SetCategoryEnabled(category, enabled)` | Sets an enabled override, preserving the level setting. |
| `ResetCategory(category)` | Removes both local overrides; explicit descendant rules remain. |

Update methods return validation errors without modifying configuration.
Resetting a valid category with no override succeeds. Each update publishes one
complete snapshot; multiple method calls are separate updates. No method writes
to the environment or a configuration file.

```go
if err := log.SetDefaultLevel(logger.WarnLevel); err != nil {
    return err
}
if err := log.SetCategoryLevel("rabbitmq.consumer", logger.DebugLevel); err != nil {
    return err
}
// Existing consumer loggers now accept debug entries.
if err := log.ResetCategory("rabbitmq.consumer"); err != nil {
    return err
}
// The consumer inherits its nearest parent's level, or warn if none overrides it.
```

## Architecture

- `internal/policy`: levels, category validation, immutable configuration values,
  and pure inheritance resolution; standard library only.
- `store.go`: atomic snapshot publication and serialized writers. Reads take no
  configuration mutex; mutable input maps and pointers are copied at construction.
- `core.go`: zap adapter consuming a small resolver interface, filtering before
  serialization, and synchronizing access to the output writer.
- `arguments.go`: lazy adaptation of arbitrary values to zap fields; JSON and
  reflection are confined to this infrastructure adapter.
- `encoder.go`: JSON formatting and recursive sensitive-key masking.
- `environment.go`: reads and validates the environment override at construction,
  outside the policy layer.
- `logger.go`, `config.go`, `fields.go`: public API and constructor wiring.

There are no global logger instances or background goroutines. Applications
inject their logger into consumers. No transport or use-case layers are needed
for this library. Output is injectable via WithOutput and WithErrorOutput. WithStacktrace adds
a stacktrace inside extra at the chosen threshold, after category filtering.

## Fields and masking

Logging methods and With accept arbitrary JSON-compatible values directly:

```go
log.Error("request failed", err)
log.Info("response received", response)
log.Info("response received", response, logger.String("request_id", "abc"))
```

For example, a response struct keeps its JSON field names:

```go
response := struct {
    Status int    `json:"status"`
    Token  string `json:"token"`
}{Status: 200, Token: "private"}
log.Info("response received", response)
```

The resulting `extra` is `{"status":200,"token":"***"}`. For
`err := errors.New("connection refused")`, `log.Error("request failed", err)`
produces `extra: {"error":"connection refused"}`. Error text is not scanned
for credentials. A direct error contributes its message, not its internal fields.

| Argument | Representation inside extra |
| --- | --- |
| Struct or map producing a JSON object | Its properties are merged directly into extra; JSON tags are respected. |
| Error | `{"error":"error message"}` |
| Scalar, slice, array, or nil | `{"value":...}` |
| zap object marshaler | Its fields are merged directly into extra. |
| Named field helper or zap field | The explicitly named field is used. |

Multiple arguments merge from left to right; later values replace earlier keys,
including fields retained by With. Use named Any fields when distinct objects or
errors must remain separate. Masking applies after merging, including nested
objects. Custom JSON/object marshalers run only for accepted entries. Values that
cannot be JSON encoded produce zap's field-encoding diagnostic in extra.

With accepts the same inputs and retains them for subsequent entries:

```go
requestLog := log.With(map[string]any{"request_id": "abc", "status": 100})
requestLog.Info("response received", response) // response.status replaces 100.
requestLog.Error("request failed", err)       // Keeps request_id and status 100.
```

Direct errors share the `error` key; direct scalars and arrays share the `value`
key, so only the last value for each key remains. To keep multiple values:

```go
log.Info("comparison", logger.Any("previous", previous), logger.Any("current", current))
log.Error("request failed", logger.Any("request_error", err), logger.Any("cleanup_error", cleanupErr))
```

With no arguments, `extra` is `{}`. An explicit nil (including a typed nil pointer)
produces `{"value":null}`. Struct serialization follows encoding/json, including
exported fields, JSON tags and custom MarshalJSON methods. An empty object has no
properties to merge. A `[]logger.Field` is treated as a group of named fields,
not as an ordinary array.

Methods now take `...any`. Existing individual field arguments still work.
For a `[]logger.Field` or `[]zap.Field`, pass the slice as one argument
(`log.Info("event", fields)`) instead of expanding it with `fields...`.
Expanded `[]any` arguments are supported. The Field alias remains a zap.Field.

Field helpers accept structured values; zap fields can also be passed directly.
Password, passwd, token, access_token, refresh_token, authorization, cookie,
secret, and api_key keys are masked case-insensitively, including nested objects
and arrays. Text inside messages or arbitrary string values is not inspected;
do not embed credentials there.

Accepted entries normalize their fields through JSON before recursive masking.
This supports reflected structs and custom zap marshalers and preserves integer
precision, at the cost of allocations on the write path. Filtered entries skip
this work entirely. With defers serialization too: retained objects must remain
valid, and callers must not mutate them concurrently with logging. Custom
marshalers must support concurrent calls if shared.

Panic and Fatal retain zap behavior (panic / process exit) even when the category
is disabled. DPanic uses zap's production behavior and does not panic. Output
errors go to the configured error writer; Sync returns wrapped errors. Call Sync
at shutdown and handle its error (some stdout devices do not support syncing).

## Validation

```sh
go test ./...
go test -race ./...
go vet ./...
go test -run '^$' -bench BenchmarkLogging -benchmem ./...
```
