# pwr CEL adapter

`github.com/lesomnus/pwr/cel` (package `pwrcel`) connects pwr readers to
`cel.dev/cel-go`. It is a separate Go module; importing the root `pwr` package
does not add CEL or ANTLR dependencies.

```sh
go get github.com/lesomnus/pwr/cel
```

Register descriptors once, compile your expression once, and pass a new or reset
reader for each evaluation:

```go
schema, err := pwr.CompileMessage(&typepb.Type{})
if err != nil { return err }
md := schema.Descriptor()
adapter, err := pwrcel.New(md)
if err != nil { return err }
options := append(adapter.EnvOptions(),
    cel.Variable("message", cel.ObjectType(string(md.FullName()))))
env, err := cel.NewEnv(options...)
if err != nil { return err }
ast, issues := env.Compile(`message.name == "Artifact"`)
if issues.Err() != nil { return issues.Err() }
program, err := env.Program(ast)
if err != nil { return err }
result, _, err := program.Eval(map[string]any{
    "message": schema.NewReader(raw),
})
```

See the [complete executable example](example_test.go) for imports and nested
access. CEL uses proto field names. Descriptors from supplied files, nested
types, and imports are registered together. `EnvOptions` copies registry metadata
for each environment construction, so options such as `cel.OptionalTypes()` and
`cel.Types(...)` do not mutate the source adapter.

## Evaluation behavior

- Scalars, nested messages, lists/maps, oneofs, `has()`, and optional access use
  pwr's field selection and presence rules. Detected wire errors become CEL
  errors, including presence tests.
- `type(message)` uses descriptor metadata without scanning ordinary message
  bytes. Ordinary field access does not unmarshal the complete message.
- `google.protobuf.Any` reads `type_url` and `value`, resolves the descriptor,
  and creates a child view sharing the parent's depth/work budget. This works
  with descriptors registered only in this adapter; global Go registration is
  unnecessary for lazy access. Unknown payload types return a CEL error.
- Other well-known types use an eager standard CEL conversion, including
  timestamps, durations, wrappers, and JSON Struct/Value/ListValue.
- Whole-message equality and `ConvertToNative` materialize and validate the
  relevant message. They enforce read limits, preserve CEL's NaN behavior, and
  use CEL's standard equality semantics. Any equality follows CEL's resolver:
  types missing from the global registry may fall back to byte comparison.
- Native protobuf inputs are also accepted. These inputs are already decoded;
  using this adapter does not recover wire-level savings for them.

The default reader handles duplicate scalar/message occurrences. Enable
`pwr.AssumeUniqueScalars()` only when the producer guarantees no duplicate
singular scalars. This allows early return and CEL short-circuit expressions to
skip later wire records. List/map contents are decoded when the collection is
requested; contained ordinary messages remain lazy. The adapter currently
allocates CEL collection views instead of promising zero allocations.

Readers and all CEL results derived from them borrow their immutable input.
Keep that input alive and unchanged until those results are discarded. A reader
must have one owner; an adapter and compiled program may be shared while each
evaluation uses independent readers. Wire/work limits and CEL expression cost
limits are separate: use `pwr.WithLimits(...)` and CEL's `cel.CostLimit(...)`
when both are needed. A raw object `ref.Val.Value()` returns its borrowed
`*pwr.Reader`; request native protobuf conversion explicitly.

## Checks

Run from this directory (the root module's `go test ./...` skips nested modules):

```sh
go test -race ./...
go vet ./...
go test -run '^$' -fuzz '^FuzzEvaluation$' -fuzztime 10s -parallel 2
```

CI also tests the adapter against the same checkout of the root module through
a temporary Go workspace.
