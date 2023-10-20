package html

import (
	"crypto/rand"
	"fmt"
	"html/template"
	"strings"
	"sync"

	"cuelang.org/go/cue"
)

var getPrefix = sync.OnceValue(func() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}
	return fmt.Sprintf("X%x_", buf[:])
})

func Format(strs []cue.Value, args []cue.Value) (string, error) {
	prefix := getPrefix()
	var buf strings.Builder
	delim0, delim1 := "{"+prefix+"{", "}"+prefix+"}"
	for i := range strs {
		// TODO bytes interpolations
		s, err := strs[i].String()
		if err != nil {
			return "", err
		}
		buf.WriteString(s)
		if i < len(args) {
			buf.WriteString(delim0)
			fmt.Fprintf(&buf, "index $ %d", i)
			buf.WriteString(delim1)
		}
	}
	t, err := template.New("").Delims(delim0, delim1).Parse(buf.String())
	if err != nil {
		return "", fmt.Errorf("cannot parse template: %v", err)
	}

	vals := make([]any, len(args))
	for i, arg := range args {
		if err := arg.Decode(&vals[i]); err != nil {
			return "", fmt.Errorf("cannot decode interpolation value %d: %v", i, err)
		}
	}
	var b strings.Builder
	if err := t.Execute(&b, vals); err != nil {
		return "", err
	}
	return b.String(), nil
}
