// Package pwr reads selected Protocol Buffers fields from borrowed wire bytes.
// Schemas reuse existing descriptors. Readers index records incrementally under
// explicit input assumptions, and merge nested messages without concatenation.
//
// Singular scalars, messages/groups, oneofs and presence are supported. Validation
// is selective. Schemas may be shared; a reader and its descendants may not be
// used concurrently. Input buffers must remain immutable while borrowed values
// are in use. See docs/design.md for semantics and limits.
package pwr
