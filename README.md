# pwr

**pwr** (proto-wire-reader) reads selected Protocol Buffers fields directly from
wire bytes, using an existing `protoreflect.MessageDescriptor` and a per-message
offset cache. It does not need a generated reader for each message type.

This is an experimental package. The API supports all singular scalar types, nested messages/groups,
oneofs, protobuf presence, repeated/packed fields, maps, bounded native conversion,
and reader reuse/pooling. The [separate CEL adapter](cel/README.md) supports
lazy field evaluation and Any payload dispatch.

## Install

Requires Go 1.23 or newer.

```sh
go get github.com/lesomnus/pwr
```

## Use

Compile a schema once and reuse it for messages of that type:

```go
schema, err := pwr.CompileMessage(&typepb.Type{})
if err != nil {
    return err
}

// raw contains the wire encoding of google.protobuf.Type.
reader := schema.NewReader(raw)
name, err := reader.String(1) // Type.name
if err != nil {
    return err
}
fmt.Println(name)
```

Imports: `github.com/lesomnus/pwr` and
`google.golang.org/protobuf/types/known/typepb`.
See the [executable example](example_test.go).

`CompileMessage` accepts generated or dynamic `proto.Message` values and uses
only their descriptor. The message's contents are not read or retained; pass a
non-nil message, such as `&typepb.Type{}`. Nil and typed nil messages return
`ErrInvalidSchema`. If you already have a descriptor, use `pwr.Compile(descriptor)`.

By default, the first supported field access scans to the end of the message
and remembers the last scalar occurrence. Later accesses use cached offsets.
If your producer guarantees that singular scalar fields never repeat, opt in
to incremental scanning:

```go
reader := schema.NewReader(raw, pwr.AssumeUniqueScalars())
```

With fields appearing as 1, 2, 3, reading field 2 scans past 1 and caches both.
A subsequent read of 1 uses the cache; reading 3 resumes after 2. Field numbers
need not appear in ascending order. A missing field scans to EOF and caches that
fact. `AssumeUniqueScalars` does not verify the producer's promise.

Nested messages use `reader.Message(number)`, returning nil for an absent field.
Duplicate singular message/group records merge without copying their payloads.
All scalar getters (`Bool`, `Enum`, `Int32`, `Int64`, `Uint32`, `Uint64`,
`Float32`, `Float64`, `String`, `Bytes`) check the descriptor type.
`WithLimits(Limits{MaxDepth: 100, MaxFields: 10000})` can bound read work.

Collections use `List(number)` / `Map(number)` or the generic `Get(number)`:

```go
items, err := reader.List(2)
if err != nil { return err }
item, err := items.Get(0)
if err != nil { return err }
child := item.Message() // for a repeated message field
_ = child
```

`reader.Reset(nextBytes)` reuses the root index. `schema.NewPool(options...)`
provides concurrent `Get(bytes)` / `Put(reader)` for exclusive per-reader ownership.
Returned child views keep their old input and budget after a root reset.
`schema.Plan(fieldNumbers...)` and `plan.Read(raw, options...)` can index a known
access set in advance. `reader.Validate()` and `reader.Materialize()` are explicit,
eager standard-decoder fallbacks for complete validation and native conversion.

## Contracts

- `Schema` is immutable and can be shared between goroutines. Each `Reader`
  belongs to one message and must not be used concurrently.
- Readers, strings, bytes, child views, collections, and CEL results borrow the
  input bytes. Keep that buffer immutable while any of those values is used.
- Missing scalars return their descriptor default. `Has` reports protobuf
  presence; `WhichOneof` reports the selected field. Proto3 optional is supported.
- Returned strings are checked for valid UTF-8. `pwr.AssumeValidUTF8()` skips
  that check; byte-boundary checks remain enabled.
- Validation is selective. Unrequested nested bodies are skipped, and early
  return may leave malformed trailing data unseen. This is not a complete
  protobuf validator, even in the default mode.
- Input must be smaller than 2 GiB. Wire/UTF-8 errors are sticky; unsupported
  or unknown field requests do not poison the reader.

## Design and experiments

- [Design decisions and nested access](docs/design.md)
- [2026-10-15 experiments: results, raw data, and reproduction](docs/experiments/2026-10-15/README.md)
- [Public API smoke benchmarks and tradeoffs](docs/experiments/2026-10-15/public-api/README.md)

The experiments measured historical prototypes, not this public package.
They include CEL adapters and native kernels that are not part of the root API.
The root module depends only on the Go protobuf module. The supported `cel/`
adapter and historical benchmark modules under `experiments/` are separate modules.

## Checks

```sh
go test -race ./...
go vet ./...
go test -run '^$' -fuzz '^FuzzReader$' -fuzztime 10s -parallel 2
python3 scripts/check_experiments.py
# Run the adapter checks separately, from cel/:
# go test -race ./... && go vet ./...
```

Apache-2.0 licensed. See [LICENSE](LICENSE).
