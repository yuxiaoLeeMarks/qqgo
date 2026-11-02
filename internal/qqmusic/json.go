package qqmusic

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// QQ Music's answers name the same thing differently from one endpoint to
// the next, as numbers here and strings there, and wrap it more or less
// deep. They are read as plain JSON values, through these helpers, which
// take the first of several names present.

type obj = map[string]any

// decode reads JSON keeping numbers as they were written: IDs run past
// what a float holds.
func decode(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	err := d.Decode(&v)
	return v, err
}

// field returns the value of the first of keys v has.
func field(v any, keys ...string) (any, bool) {
	m, ok := v.(obj)
	if !ok {
		return nil, false
	}
	for _, k := range keys {
		if x, ok := m[k]; ok {
			return x, true
		}
	}
	return nil, false
}

// get follows a path of keys into v: each step an object's key, or a
// number for an array's index.
func get(v any, path ...string) any {
	for _, k := range path {
		switch x := v.(type) {
		case obj:
			v = x[k]
		case []any:
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

// str returns the first of keys as a string, a number written out.
func str(v any, keys ...string) string {
	x, _ := field(v, keys...)
	return asString(x)
}

func asString(x any) string {
	switch x := x.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return ""
}

// num returns the first of keys as an integer, from a number or a string
// of one, or 0.
func num(v any, keys ...string) int64 {
	x, _ := field(v, keys...)
	return asInt(x)
}

func asInt(x any) int64 {
	switch x := x.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		if f, err := x.Float64(); err == nil {
			return int64(f)
		}
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

// float returns the first of keys as a number, or 0.
func float(v any, keys ...string) float64 {
	x, _ := field(v, keys...)
	switch x := x.(type) {
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

// flag returns the first of keys as a boolean: true, 1 or "1".
func flag(v any, keys ...string) bool {
	x, _ := field(v, keys...)
	switch x := x.(type) {
	case bool:
		return x
	case json.Number, string:
		return asInt(x) != 0 || asString(x) == "true"
	}
	return false
}

// arr returns the first of keys as an array, or nil.
func arr(v any, keys ...string) []any {
	x, _ := field(v, keys...)
	a, _ := x.([]any)
	return a
}

// find looks for the first of keys anywhere in v, nearest first, ignoring
// case, whose value ok accepts. An object's keys go in order, so that what
// it finds is the same each time.
func find(v any, ok func(any) bool, keys ...string) any {
	switch x := v.(type) {
	case obj:
		names := slices.Sorted(maps.Keys(x))
		for _, k := range names {
			for _, key := range keys {
				if strings.EqualFold(k, key) && ok(x[k]) {
					return x[k]
				}
			}
		}
		for _, k := range names {
			if found := find(x[k], ok, keys...); found != nil {
				return found
			}
		}
	case []any:
		for _, val := range x {
			if found := find(val, ok, keys...); found != nil {
				return found
			}
		}
	}
	return nil
}

func isArray(v any) bool  { _, ok := v.([]any); return ok }
func isObject(v any) bool { _, ok := v.(obj); return ok }
func isText(v any) bool {
	s := asString(v)
	return s != "" && strings.TrimSpace(s) != ""
}

// findArr finds an array named one of keys anywhere in v.
func findArr(v any, keys ...string) []any {
	a, _ := find(v, isArray, keys...).([]any)
	return a
}

// findStr finds a string, or a number, named one of keys anywhere in v.
func findStr(v any, keys ...string) string {
	return asString(find(v, isText, keys...))
}

// https makes an image's address one the app fetches: QQ gives some as
// http, or without a scheme.
func https(u string) string {
	u = strings.TrimSpace(u)
	switch {
	case strings.HasPrefix(u, "http://"):
		return "https://" + u[len("http://"):]
	case strings.HasPrefix(u, "//"):
		return "https:" + u
	}
	return u
}
