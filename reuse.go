package pwr

import (
	"errors"
	"sync"
)

// ErrPoolOwnership reports a foreign reader or an already returned reader.
var ErrPoolOwnership = errors.New("pwr: invalid pool ownership")

// Reset reuses a root Reader's index allocation with the same schema/options.
// The caller must exclusively own the Reader. Previously obtained child readers,
// lists, maps, strings and bytes remain tied to their OLD input and budget; keep
// that input immutable while those values are used. A pooled reader must not be
// used after Put. Reset is not supported on child readers.
func (r *Reader) Reset(raw []byte) error {
	if r == nil || r.depth != 0 || r.returned {
		return ErrPoolOwnership
	}
	schema, first, utf8, limits := r.schema, r.first, r.assumeUTF8, r.limits
	index, active, cache := r.index, r.active, r.cache
	clear(index)
	clear(active)
	clear(cache)
	owner := r.owner
	*r = Reader{schema: schema, raw: raw, size: len(raw), first: first, assumeUTF8: utf8, limits: limits, index: index, active: active, cache: cache, owner: owner}
	r.initialize()
	return r.err
}

// Pool reuses root Readers for one schema and fixed options. A Pool is safe for
// concurrent Get/Put; each checked-out Reader still has one exclusive owner.
// Do not copy a Pool after use. Retention follows sync.Pool and is not guaranteed.
type Pool struct {
	schema *Schema
	config *Reader
	pool   sync.Pool
}

// NewPool captures options once. Option callbacks are not run concurrently.
func (s *Schema) NewPool(options ...ReaderOption) *Pool {
	return &Pool{schema: s, config: s.NewReader(nil, options...)}
}

// Get returns an exclusively owned reader. Accessors report invalid input/options.
func (p *Pool) Get(raw []byte) *Reader {
	if v := p.pool.Get(); v != nil {
		r := v.(*Reader)
		r.returned = false
		_ = r.Reset(raw)
		return r
	}
	c := p.config
	r := &Reader{schema: p.schema, raw: raw, size: len(raw), first: c.first, assumeUTF8: c.assumeUTF8, limits: c.limits, owner: p}
	r.initialize()
	return r
}

// Put releases a reader and all references it holds to input and child views.
// The reader must have come from this Pool and may be returned only once per Get.
// Previously obtained child views have independent lifetimes; they are not pooled.
func (p *Pool) Put(r *Reader) error {
	if r == nil || r.owner != p || r.returned {
		return ErrPoolOwnership
	}
	_ = r.Reset(nil)
	r.returned = true
	p.pool.Put(r)
	return nil
}
