package envflag

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// Init initializes the fields in flags from the attached struct field tags
// as well as the contents of the given environment variable.
//
// The struct field tag may contain a default value other than the zero value,
// such as `envflag:"default:true"` to set a boolean field to true by default.
//
// The environment variable may contain a comma-separated list of name=value
// pairs values representing the boolean fields in the struct type T.
// If the value is omitted entirely, the value is assumed to be name=true.
//
// Names are treated case insensitively.
// Value strings are parsed as Go booleans via [strconv.ParseBool],
// meaning that they accept "true" and "false" but also the shorter "1" and "0".
func Init[T any](flags *T, envVar string) error {
	// Collect the field indices and set the default values.
	indexByName := make(map[string]int)
	fv := reflect.ValueOf(flags).Elem()
	ft := fv.Type()
	for i := 0; i < ft.NumField(); i++ {
		field := ft.Field(i)
		defaultValue := false
		if tagStr, ok := field.Tag.Lookup("envflag"); ok {
			defaultStr, ok := strings.CutPrefix(tagStr, "default:")
			if !ok {
				return fmt.Errorf("expected tag like `envflag:\"default:true\"`: %s", tagStr)
			}
			v, err := strconv.ParseBool(defaultStr)
			if err != nil {
				return fmt.Errorf("invalid default bool value for %s: %v", field.Name, err)
			}
			defaultValue = v
		}
		fv.Field(i).SetBool(defaultValue)
		indexByName[strings.ToLower(field.Name)] = i
	}

	// Parse the env value to set the fields.
	env := os.Getenv(envVar)
	if env == "" {
		return nil
	}
	for _, elem := range strings.Split(env, ",") {
		name, valueStr, ok := strings.Cut(elem, "=")
		// "somename" is short for "somename=true" or "somename=1".
		value := true
		if ok {
			v, err := strconv.ParseBool(valueStr)
			if err != nil {
				return fmt.Errorf("invalid bool value for %s: %v", name, err)
			}
			value = v
		}
		index, ok := indexByName[name]
		if !ok {
			return fmt.Errorf("unknown %s %s", envVar, elem)
		}
		fv.Field(index).SetBool(value)
	}
	return nil
}
