// Package pwr reads selected Protocol Buffers fields from borrowed wire bytes.
// Schemas reuse existing descriptors. Readers index records incrementally under
// explicit input assumptions, and merge nested messages without concatenation.
//
// Scalars, messages/groups, oneofs, presence, repeated/packed fields and maps are
// supported. Readers can be reset or pooled. Ordinary validation
// is selective. Schemas may be shared; a reader and its descendants may not be
// used concurrently. Input buffers must remain immutable while borrowed values
// are in use. See docs/design.md for semantics and limits.
package pwr
