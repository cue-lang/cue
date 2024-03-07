package envflag

import (
	"fmt"
	"os"
	"reflect"
	"strings"
)

// Init initializes the fields in flags from the contents of the given
// environment variable, which contains a comma-separated
// list of names representing the boolean fields in the struct type T.
// Names are treated case insensitively.
func Init[T any](flags *T, envVar string) error {
	env := os.Getenv(envVar)
	if env == "" {
		return nil
	}
	names := make(map[string]int)
	fv := reflect.ValueOf(flags).Elem()
	ft := fv.Type()
	for i := 0; i < ft.NumField(); i++ {
		names[strings.ToLower(ft.Field(i).Name)] = i
	}
	for _, name := range strings.Split(env, ",") {
		index, ok := names[name]
		if !ok {
			return fmt.Errorf("unknown %s %s", envVar, name)
		}
		fv.Field(index).SetBool(true)
	}
	return nil
}
