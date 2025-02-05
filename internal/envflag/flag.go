package envflag

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// Init uses Parse with the contents of the given environment variable as input.
func Init[T any](flags *T, envVar string) error {
	err := Parse(flags, os.Getenv(envVar))
	if err != nil {
		return fmt.Errorf("cannot parse %s: %w", envVar, err)
	}
	return nil
}

// Parse initializes the fields in flags from the attached struct field tags as
// well as the contents of the given string.
//
// The struct field tag may contain a default value other than the zero value,
// such as `envflag:"default:true"` to set a boolean field to true by default.
//
// The tag may be marked as deprecated with `envflag:"deprecated"`
// which will cause Parse to return an error if the user attempts to set
// its value to anything but the default value.
//
// The string may contain a comma-separated list of name=value pairs values
// representing the boolean fields in the struct type T. If the value is omitted
// entirely, the value is assumed to be name=true.
//
// Names are treated case insensitively. Boolean values are parsed via [strconv.ParseBool],
// integers via [strconv.Atoi], and strings are accepted as-is.
func Parse[T any](flags *T, env string) error {
	// Collect the field indices and set the default values.
	indexByName := make(map[string]int)
	deprecated := make(map[string]bool)
	fv := reflect.ValueOf(flags).Elem()
	ft := fv.Type()
	for i := 0; i < ft.NumField(); i++ {
		field := ft.Field(i)
		name := strings.ToLower(field.Name)
		if tagStr, ok := field.Tag.Lookup("envflag"); ok {
			for _, f := range strings.Split(tagStr, ",") {
				key, rest, hasRest := strings.Cut(f, ":")
				switch key {
				case "default":
					val, err := parseValue(name, field.Type.Kind(), rest)
					if err != nil {
						return err
					}
					fv.Field(i).Set(reflect.ValueOf(val))
				case "deprecated":
					if hasRest {
						return fmt.Errorf("cannot have a value for deprecated tag")
					}
					deprecated[name] = true
				default:
					return fmt.Errorf("unknown envflag tag %q", f)
				}
			}
		}
		indexByName[name] = i
	}

	var errs []error
	for _, elem := range strings.Split(env, ",") {
		if elem == "" {
			// Allow empty elements such as `,somename=true` so that env vars
			// can be joined together like
			//
			//     os.Setenv("CUE_EXPERIMENT", os.Getenv("CUE_EXPERIMENT")+",extra")
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
			// This mimicks how Go flags work, e.g. -knob is short for -knob=true.
			val = true
		} else {
			// For any other type, a value must be specified.
			// This mimicks how Go flags work, e.g. -output=path does not allow -output.
			errs = append(errs, fmt.Errorf("value needed for %s flag %q", field.Kind(), name))
			continue
		}

		if deprecated[name] {
			// We allow setting deprecated flags to their default value so that
			// bold explorers will not be penalised for their experimentation.
			if field.Interface() != val {
				errs = append(errs, fmt.Errorf("cannot change default value of deprecated flag %q", name))
			}
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
	case reflect.String:
		val = str
	default:
		return nil, errInvalid{fmt.Errorf("unsupported kind %s", kind)}
	}
	if err != nil {
		return nil, errInvalid{fmt.Errorf("invalid %s value for %s: %v", kind, name, err)}
	}
	return val, nil
}

// An ErrInvalid indicates a malformed input string.
var ErrInvalid = errors.New("invalid value")

type errInvalid struct{ error }

func (errInvalid) Is(err error) bool {
	return err == ErrInvalid
}
