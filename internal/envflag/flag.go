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
// The string may contain a comma-separated list of name=value pairs values
// representing the boolean fields in the struct type T. If the value is omitted
// entirely, the value is assumed to be name=true.
//
// Names are treated case insensitively. Value strings are parsed as Go booleans
// via [strconv.ParseBool], meaning that they accept "true" and "false" but also
// the shorter "1" and "0".
func Parse[T any](flags *T, env string) error {
	// Collect the field indices and set the default values.
	indexByName := make(map[string]int)
	fv := reflect.ValueOf(flags).Elem()
	ft := fv.Type()
	for i := 0; i < ft.NumField(); i++ {
		field := ft.Field(i)
		defaultValue := false
		if tagStr, ok := field.Tag.Lookup("envflag"); ok {
			defaultStr, ok := strings.CutPrefix(tagStr, "default:")
			// TODO: consider panicking for these error types.
			if !ok {
				return fmt.Errorf("expected tag like `envflag:\"default:true\"`: %s", tagStr)
			}
			v, err := strconv.ParseBool(defaultStr)
			if err != nil {
				return fmt.Errorf("invalid default bool value for %s: %v", field.Name, err)
			}
			defaultValue = v
			fv.Field(i).SetBool(defaultValue)
		}
		indexByName[strings.ToLower(field.Name)] = i
	}

	if env == "" {
		return nil
	}
	var errs []error
	for _, elem := range strings.Split(env, ",") {
		name, valueStr, ok := strings.Cut(elem, "=")
		// "somename" is short for "somename=true" or "somename=1".
		value := true
		if ok {
			v, err := strconv.ParseBool(valueStr)
			if err != nil {
				// Invalid format, return an error immediately.
				return invalidError{
					fmt.Errorf("invalid bool value for %s: %v", name, err),
				}
			}
			value = v
		}
		index, ok := indexByName[name]
		if !ok {
			// Unknown option, proceed processing options as long as the format
			// is valid.
			errs = append(errs, fmt.Errorf("unknown %s", elem))
			continue
		}
		fv.Field(index).SetBool(value)
	}
	return errors.Join(errs...)
}

// An InvalidError indicates a malformed input string.
var InvalidError = errors.New("invalid value")

type invalidError struct{ error }

func (invalidError) Is(err error) bool {
	return err == InvalidError
}
