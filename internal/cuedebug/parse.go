package cuedebug

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Parse parses a CUE_DEBUG string into a [Config].
func Parse(s string) (Config, error) {
	var c Config
	err := parse(&c, s)
	return c, err
}

// parse initializes the fields in flags from the attached struct field tags as
// well as the contents of the given string.
//
// The struct field tag may contain a default value other than the zero value,
// such as `cuedebug:"default:true"` to set a boolean field to true by default.
//
// The string may contain a comma-separated list of name=value pairs
// for the fields of T. For a boolean field, a bare name means name=true.
//
// Boolean values are parsed via [strconv.ParseBool], and integers via [strconv.Atoi].
func parse[T any](flags *T, env string) error {
	// Collect the field indices and set the default values.
	indexByName := make(map[string]int)
	fv := reflect.ValueOf(flags).Elem()
	ft := fv.Type()
	for i := range ft.NumField() {
		field := ft.Field(i)
		name := strings.ToLower(field.Name)
		if tag, ok := field.Tag.Lookup("cuedebug"); ok {
			def, ok := strings.CutPrefix(tag, "default:")
			if !ok {
				panic(fmt.Errorf("unknown cuedebug tag %q", tag))
			}
			val, err := parseValue(name, field.Type.Kind(), def)
			if err != nil {
				panic(err)
			}
			fv.Field(i).Set(reflect.ValueOf(val))
		}
		indexByName[name] = i
	}

	var errs []error
	for elem := range strings.SplitSeq(env, ",") {
		if elem == "" {
			// Allow empty elements such as `,somename=true` so that env vars
			// can be joined together like
			//
			//     os.Setenv("CUE_DEBUG", os.Getenv("CUE_DEBUG")+",extra")
			//
			// even when the previous env var is empty.
			continue
		}
		name, valueStr, hasValue := strings.Cut(elem, "=")

		index, knownFlag := indexByName[name]
		if !knownFlag {
			errs = append(errs, fmt.Errorf("unknown flag %q", elem))
			continue
		}
		field := fv.Field(index)
		var val any
		if hasValue {
			var err error
			val, err = parseValue(name, field.Kind(), valueStr)
			if err != nil {
				errs = append(errs, err)
				continue
			}
		} else if field.Kind() == reflect.Bool {
			// For bools, "somename" is short for "somename=true" or "somename=1".
			// This mimics how Go flags work, e.g. -knob is short for -knob=true.
			val = true
		} else {
			// For any other type, a value must be specified.
			// This mimics how Go flags work, e.g. -output=path does not allow -output.
			errs = append(errs, fmt.Errorf("value needed for %s flag %q", field.Kind(), name))
			continue
		}

		field.Set(reflect.ValueOf(val))
	}
	return errors.Join(errs...)
}

func parseValue(name string, kind reflect.Kind, str string) (val any, err error) {
	switch kind {
	case reflect.Bool:
		val, err = strconv.ParseBool(str)
	case reflect.Int:
		val, err = strconv.Atoi(str)
	default:
		panic(fmt.Errorf("unsupported kind %s", kind))
	}
	if err != nil {
		return nil, fmt.Errorf("invalid %s value for %s: %v", kind, name, err)
	}
	return val, nil
}
