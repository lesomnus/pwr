# pwr

**pwr** (proto-wire-reader) reads selected Protocol Buffers fields directly from
wire bytes, using an existing `protoreflect.MessageDescriptor` and a per-message
offset cache. It does not need a generated reader for each message type.

This is an experimental package. The current API reads **top-level singular
strings outside real oneofs**. Nested messages, other scalar getters,
collections, presence, and a CEL adapter are not implemented yet.

## Install

Requires Go 1.23 or newer.

```sh
go get github.com/lesomnus/pwr
```

## Use

Compile a schema once and reuse it for messages of that type:

```go
schema, err := pwr.Compile((&typepb.Type{}).ProtoReflect().Descriptor())
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

## Contracts

- `Schema` is immutable and can be shared between goroutines. Each `Reader`
  belongs to one message and must not be used concurrently.
- The reader and returned strings borrow the input bytes. Keep that buffer
  immutable and alive while **either the reader or any returned string** is used.
- Missing strings return the descriptor default (usually `""`). Presence is
  not reported. Proto3 optional strings are supported.
- Returned strings are checked for valid UTF-8. `pwr.AssumeValidUTF8()` skips
  that check; byte-boundary checks remain enabled.
- Validation is selective. Unrequested nested bodies are skipped, and early
  return may leave malformed trailing data unseen. This is not a complete
  protobuf validator, even in the default mode.
- Input must be smaller than 2 GiB. Wire/UTF-8 errors are sticky; unsupported
  or unknown field requests do not poison the reader.

## Design and experiments

- [Design decisions and planned nested access](docs/design.md)
- [2026-10-15 experiments: results, raw data, and reproduction](docs/experiments/2026-10-15/README.md)

The experiments measured historical prototypes, not this public package.
They include CEL adapters and native kernels that are not part of the root API.
The root module depends only on the Go protobuf module; historical benchmark
modules and their dependencies are isolated under `experiments/`.

## Checks

```sh
go test -race ./...
go vet ./...
go test -run '^$' -fuzz '^FuzzReader$' -fuzztime 10s -parallel 2
python3 scripts/check_experiments.py
```

Apache-2.0 licensed. See [LICENSE](LICENSE).
