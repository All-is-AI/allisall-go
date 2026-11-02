package allisall

import (
	"bytes"
	"encoding/json"
)

// Nullable is a member of a merge patch that can be cleared. It holds one
// of three states:
//
//   - absent, the zero Nullable: the member is not sent, and the API leaves
//     the value as it is;
//   - null, from [Null]: the member is sent as null, and the API clears the
//     value;
//   - a value, from [NewNullable]: the member is sent with that value.
//
// A struct member of this type is tagged omitzero, which leaves out an
// absent member. Outside such a member, an absent Nullable is written as
// null.
type Nullable[T any] struct {
	value T
	state nullableState
}

type nullableState uint8

const (
	nullableAbsent nullableState = iota
	nullableNull
	nullableSet
)

// NewNullable returns a Nullable holding v.
func NewNullable[T any](v T) Nullable[T] {
	return Nullable[T]{value: v, state: nullableSet}
}

// Null returns a Nullable holding an explicit null.
func Null[T any]() Nullable[T] {
	return Nullable[T]{state: nullableNull}
}

// Get returns the value, and true only when one is set. When n is absent or
// null, it returns the zero T and false.
func (n Nullable[T]) Get() (T, bool) {
	if n.state != nullableSet {
		var zero T
		return zero, false
	}
	return n.value, true
}

// IsNull reports whether n holds an explicit null.
func (n Nullable[T]) IsNull() bool { return n.state == nullableNull }

// IsZero reports whether n is absent. The omitzero tag option reads it, so
// an absent member is not sent.
func (n Nullable[T]) IsZero() bool { return n.state == nullableAbsent }

// setValue returns the value, and true only when one is set, for the
// request body's UTF-8 check, which must see what MarshalJSON writes.
func (n Nullable[T]) setValue() (any, bool) {
	if n.state != nullableSet {
		return nil, false
	}
	return n.value, true
}

// MarshalJSON writes null, or the value's own JSON.
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if n.state != nullableSet {
		return []byte("null"), nil
	}
	return json.Marshal(n.value)
}

// UnmarshalJSON reads null as an explicit null, and anything else as a set
// value.
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*n = Null[T]()
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*n = NewNullable(v)
	return nil
}
