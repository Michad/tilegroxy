# AGENTS.md

Guidance for AI coding agents working in the tilegroxy repository.

## Rules

These apply to every change. Follow them without being asked.

1. **Never commit, stage, push, or open a pull request on your own.** Leave changes in the
   working tree and tell the user what you did. Read-only git commands are always fine. Do
   these only when explicitly asked, and only for the change being asked about.
2. **Do not add the current user's name to copyright headers** when a file's changes are
   entirely AI generated. Leave the existing `// Copyright YYYY ...` line alone. Attribution is
   the user's call.
3. **Always document new configuration, behaviors, or capabilities**, in the same change. A new
   config key, provider, cache, CLI flag, or a change to how an existing option behaves is not
   finished until `docs/` reflects it.
4. **Match the surrounding writing style in documentation.** Read the peer pages beside yours
   in `nav.adoc` first, match their layout and tone exactly, and compare again when done. Avoid
   AI cliches. No em-dashes.
5. **Do not lower code coverage.** Up to 0.3% jitter is fine since the calculation varies
   between runs. Anything more means the change needs tests.
6. **Always run `make lint` and apply everything it reports**, advisory suggestions included.
   Fix findings rather than suppressing them with `//nolint`.
7. **Avoid breaking backwards compatibility.** See the contract below. When a break looks
   unavoidable, explain what breaks and let the user decide.
8. **Keep comments short.** Explain the why as simply as possible. Anything longer than a
   sentence, or that affects more than one place in the code, belongs in the development
   documentation instead.
9. **Update the comments and docs your change impacts.** A stale comment is worse than none.
10. **Do not generate ADRs** ADRs are for humans making decisions, never generate one as an AI Agent.

Otherwise: match the idioms of the code you are editing, keep changes focused on what was
asked, and add tests alongside behavior changes.

## Architecture

Tilegroxy is a Go tile server and proxy. It serves map tiles for configured layers, pulling
data from pluggable Providers and storing results in pluggable Caches. Everything meaningful is
driven by an operator-supplied YAML/JSON config.

A request flows: HTTP handler, layer lookup by name or pattern, cache check, then the layer's
Provider tree, which may nest (`ref`, `fallback`, `blend`) before reaching something that
fetches or renders imagery.

Seven entity kinds are pluggable and all follow one registration pattern: **provider, cache,
authentication, analytics, datastore, secret, health check**. Implementations live in
`internal/` (`providers/`, `caches/`, `authentications/`, `analytics/`, `datastores/`,
`secrets/`, `checks/`), with the interface and `Register*` function for each in
`pkg/entities/`. To add one, copy the shape in
[internal/providers/proxy.go](internal/providers/proxy.go): a `XConfig` struct, an `X` struct,
`func init()` calling `RegisterProvider(XRegistration{})`, and a registration type with
`InitializeConfig`, `Name`, and `Initialize`. `Initialize` does all validation and must not
panic or defer validation to request time.

### Package layout is a stability contract

- `pkg/` is the public API for extending tilegroxy. Any deletion, rename, or signature change
  here is a breaking change. Keep it minimal; utility code does not belong here.
- `internal/` holds the majority of logic. Safe to change freely.
- `cmd/` is Cobra scaffolding only. Logic belongs in `pkg/` or `internal/`.
- Root holds only what must be there. `_scratch/` is gitignored, use it for throwaway files.

Backwards compatibility covers four surfaces: the `pkg/` API, the configuration schema
(operators have YAML in production), the CLI, and the custom provider/auth/analytics function
signatures that operator-written Yaegi scripts compile against. Prefer adding a new optional
key or function over changing an existing one.

### Two conventions that are easy to miss

**Config validation errors use `config.ErrorMessages` format strings**, not string literals, so
operators can localize them. The first argument is the dotted config path:

```go
return nil, fmt.Errorf(errorMessages.InvalidParam, "provider.proxy.url", "")
```

**Operator input is trusted, User input is hostile.** Operator input is the configuration; it
may reach SQL construction in a database-backed provider. User input is anything on an incoming
tile request (layer name, coordinates, headers, tokens) and is treated as malicious. Layer names
are sanitized before use as cache keys. When adding config that flows into a request path, check
which side each value is on.

## Commands

Everything goes through the Makefile. Do not hand-roll `go build` or `go test`, the build tag
and ldflags matter.

```
make            # clean, test, docs, build, version (the full check)
make test       # go test with -tags viper_bind_struct
make unit       # unit only, excludes testcontainer integration tests
make e2e        # end-to-end tests against the compiled binary, builds docs and binary first
make lint       # golangci-lint with --fix
make cover      # coverage via Courtney, writes coveragef.out
make docs       # Antora build, required before `make build` works
make readme     # regenerates README.adoc, never edit that file directly
```

`make build` embeds `internal/website/resources`, which is gitignored build output produced by
`make docs`. `make docs` needs node 20 and network access, so it can fail in a sandbox. Say so
if it does rather than skipping it silently.

Every `.go` file needs the Apache 2.0 header from `.preamble.txt`; the `goheader` linter
enforces it. Copy it from a neighboring file.

## Read the docs for details

`docs/` is AsciiDoc built by Antora into three modules. Consult them rather than guessing, and
prefer them over this file when they conflict:

- **`docs/development/modules/ROOT/pages/`** for contributor detail: `repository.adoc` (the
  authoritative layout rules), `tests.adoc` (integration test build tags, the >85% coverage
  target, where end-to-end tests belong), `documentation.adoc` (docs and README build),
  `glossary.adoc` (domain terms, including the Operator/User distinction above),
  `troubleshooting.adoc` (Docker and testcontainer problems).
- **`docs/operation/modules/ROOT/pages/`** for user-facing behavior: `configuration/` for every
  config option, `extensibility.adoc` for the custom Yaegi interfaces and library use,
  `security.adoc`, `telemetry.adoc`.
- **`docs/decisions/modules/ROOT/pages/`** for ADRs. Significant architectural choices get one,
  using `0000-template.adoc`.

New config pages go under `docs/operation/modules/ROOT/pages/configuration/` and must be added
to the matching `nav.adoc`. Links are checked in CI, so a broken `xref:` fails the build.

Also worth knowing: `.golangci.yml` for the linter set and its exemptions, and `.github/workflows/`
for what CI actually runs.
