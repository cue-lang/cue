package cueexperiment

import (
	"fmt"
	"os"
	"reflect"
	"strings"
)

// Flags holds the set of CUE_EXPERIMENT flags. It is initialized
// by Init.
var Flags struct {
	Modules bool
}

// Init initializes Flags. Note: this isn't named "init" because we
// don't always want it to be called (for example we don't want it to be
// called when running "cue help"), and also because we want the failure
// mode to be one of error not panic, which would be the only option if
// it was a top level init function
func Init() error {
	exp := os.Getenv("CUE_EXPERIMENT")
	if exp == "" {
		return nil
	}
	names := make(map[string]int)
	fv := reflect.ValueOf(&Flags).Elem()
	ft := fv.Type()
	for i := 0; i < ft.NumField(); i++ {
		names[strings.ToLower(ft.Field(i).Name)] = i
	}
	for _, uexp := range strings.Split(exp, ",") {
		index, ok := names[uexp]
		if !ok {
			return fmt.Errorf("unknown CUE_EXPERIMENT %s", uexp)
		}
		fv.Field(index).SetBool(true)
	}
	return nil
}
