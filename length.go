package jutil

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Lengther can be implemented by a value to override the default length
// deduction algorithm implemented by Length.
type Lengther interface {
	// LengthJSON returns the length of the value once serialized to JSON.
	LengthJSON() int
}

// Length computes the length of the JSON representation of a value of
// arbitrary type, it's ~10x faster than serializing the content with the
// standard json package and avoid the extra memory allocations.
func Length(v interface{}) (n int, err error) {
	var b []byte

	if v == nil {
		n = jsonLenNull()
		return
	}

	// Fast path for base types, this is has shown to be faster than using
	// reflection in the benchmarks.
	switch x := v.(type) {
	case bool:
		n = jsonLenBool(x)

	case int:
		n = jsonLenInt(int64(x))

	case int8:
		n = jsonLenInt(int64(x))

	case int16:
		n = jsonLenInt(int64(x))

	case int32:
		n = jsonLenInt(int64(x))

	case int64:
		n = jsonLenInt(int64(x))

	case uint:
		n = jsonLenUint(uint64(x))

	case uint8:
		n = jsonLenUint(uint64(x))

	case uint16:
		n = jsonLenUint(uint64(x))

	case uint32:
		n = jsonLenUint(uint64(x))

	case uint64:
		n = jsonLenUint(uint64(x))

	case float32:
		n = jsonLenFloat(float64(x))

	case float64:
		n = jsonLenFloat(float64(x))

	case string:
		n = jsonLenString(x)

	case []byte:
		n = jsonLenBytes(x)

	case map[string]interface{}:
		n, err = jsonLenMapStringInterface(x)

	case []interface{}:
		n, err = jsonLenSliceInterface(x)

	case Lengther:
		n = x.LengthJSON()

	case json.Number:
		n = len(x)

	case json.Marshaler:
		if b, err = x.MarshalJSON(); err == nil {
			n = len(b)
		}

	case encoding.TextMarshaler:
		if b, err = x.MarshalText(); err == nil {
			n = jsonLenString(string(b))
		}

	default:
		n, err = jsonLenV(reflect.ValueOf(v))
	}

	return
}

func jsonLenV(v reflect.Value) (n int, err error) {
	if !v.IsValid() {
		err = &json.UnsupportedValueError{Value: v, Str: "the value is invalid"}
		return
	}

	switch t := v.Type(); t.Kind() {
	case reflect.Struct:
		n, err = jsonLenStruct(t, v)

	case reflect.Map:
		n, err = jsonLenMap(v)

	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			n = jsonLenBytes(v.Bytes()) // []byte
		} else {
			n, err = jsonLenArray(v)
		}

	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			n = jsonLenNull()
		} else {
			elem := v.Elem()
			if !elem.CanInterface() {
				err = fmt.Errorf("reflect: cannot call Interface on %v", elem)
				return
			}
			n, err = Length(elem.Interface())
		}

	case reflect.Bool:
		n = jsonLenBool(v.Bool())

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n = jsonLenInt(v.Int())

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n = jsonLenUint(v.Uint())

	case reflect.Float32, reflect.Float64:
		n = jsonLenFloat(v.Float())

	case reflect.String:
		n = jsonLenString(v.String())

	case reflect.Array:
		n, err = jsonLenArray(v)

	default:
		err = &json.UnsupportedTypeError{Type: t}
	}

	return
}

func jsonLenNull() (n int) {
	return 4
}

func jsonLenBool(v bool) (n int) {
	if v {
		return 4
	}
	return 5
}

func jsonLenInt(v int64) (n int) {
	if v == 0 {
		return 1
	}
	if v < 0 {
		n++
	}
	for v != 0 {
		v /= 10
		n++
	}
	return
}

func jsonLenUint(v uint64) (n int) {
	if v == 0 {
		return 1
	}
	for v != 0 {
		v /= 10
		n++
	}
	return
}

func jsonLenFloat(v float64) (n int) {
	var b [32]byte
	return len(strconv.AppendFloat(b[:0], v, 'g', -1, 64))
}

func jsonLenString(s string) (n int) {
	if len(s) < 100 {
		// This is an optimization for short strings, most of them will not
		// contain any escaped bytes and performances won't suffer from checking
		// the string a couple of times because it will fit into a couple of
		// cachelines.
		const escapedBytes = "/\"\\\n\t\r\v\b\f"

		switch len(s) {
		case 0:
		case 1:
			for i := range escapedBytes {
				if s[0] == escapedBytes[i] {
					goto slowPath
				}
			}
		default:
			for i := range escapedBytes {
				if strings.IndexByte(s, escapedBytes[i]) >= 0 {
					goto slowPath
				}
			}
		}

		return 2 + len(s)
	}

slowPath:
	for i := range s {
		switch s[i] {
		case '\n', '\t', '\r', '\v', '\b', '\f', '\\', '/', '"':
			n++
		}
	}

	return n + 2 + len(s)
}

func jsonLenBytes(b []byte) (n int) {
	// The standard json package uses base64 encoding for byte slices...
	return 2 + ((len(b) * 4) / 3)
}

func jsonLenArray(v reflect.Value) (n int, err error) {
	var c int

	for i, j := 0, v.Len(); i != j; i++ {
		if i != 0 {
			n++
		}

		elem := v.Index(i)
		if !elem.CanInterface() {
			err = fmt.Errorf("reflect: cannot call Interface on value %v", elem)
			return
		}
		if c, err = Length(elem.Interface()); err != nil {
			return
		}

		n += c
	}

	n += 2
	return
}

func jsonLenMap(v reflect.Value) (n int, err error) {
	var c1 int
	var c2 int

	for i, k := range v.MapKeys() {
		if !k.CanInterface() {
			err = fmt.Errorf("reflect: cannot call Interface on value %v", k)
			return
		}

		if i != 0 {
			n++
		}

		if c1, err = Length(k.Interface()); err != nil {
			return
		}

		if c2, err = Length(v.MapIndex(k).Interface()); err != nil {
			return
		}

		n += c1 + c2 + 1
	}

	n += 2
	return
}

func jsonLenStruct(t reflect.Type, v reflect.Value) (n int, err error) {
	var c int
	var s = LookupStruct(t)

	for _, f := range s {
		fv := v.FieldByIndex(f.Index)

		if f.Omitempty && isEmptyValue(fv) {
			continue
		}

		if !fv.CanInterface() {
			err = fmt.Errorf("reflect: cannot call Interface on %v", fv)
			return
		}
		if c, err = Length(fv.Interface()); err != nil {
			return
		}

		if n != 0 {
			n++
		}

		n += jsonLenString(f.Name) + c + 1
	}

	n += 2
	return
}

func jsonLenSliceInterface(s []interface{}) (n int, err error) {
	var c int

	for _, v := range s {
		if c, err = Length(v); err != nil {
			return
		}
		n += c
	}

	if c = len(s); c > 1 {
		n += c - 1
	}

	n += 2
	return
}

func jsonLenMapStringInterface(m map[string]interface{}) (n int, err error) {
	var c int

	for k, v := range m {
		if c, err = Length(v); err != nil {
			return
		}
		n += jsonLenString(k) + c + 1
	}

	if c = len(m); c > 1 {
		n += c - 1
	}

	n += 2
	return
}
