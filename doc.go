// Package pwr reads selected Protocol Buffers fields from wire bytes using a
// precompiled descriptor and a per-message offset index.
//
// This experimental version exposes only singular string fields outside real
// oneofs. It does not implement nested access, collections, a presence API, or
// complete protobuf validation. See the repository's docs/design.md for scope.
//
// A Schema is immutable and may be shared. A Reader belongs to one message and
// must not be used concurrently. Readers and returned strings borrow the input;
// callers must not modify or recycle that buffer while either is in use.
package pwr
