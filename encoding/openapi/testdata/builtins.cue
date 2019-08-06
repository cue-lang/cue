import (
	"time"
	"list"
)

_time = time

MyStruct: {
	timestamp1?: time.Time
	timestamp2?: time.Time()
	timestamp3?: time.Format(time.RFC3339Nano)
	timestamp4?: _time.Time
	date1?:      time.Format(time.RFC3339Date)
	date2?:      _time.Format(time.RFC3339Date)

	// This is not an OpenAPI type and has no format. In this case
	// we map to a type so that it can be documented properly (without
	// repeating it).
	timeout?: time.Duration

	contains: list.Contains("foo") // not supported, but should be recognized as list
}
