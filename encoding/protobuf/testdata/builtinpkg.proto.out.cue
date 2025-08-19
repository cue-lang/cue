package builtinpkg

import "time"

#DefaultPkgTestMsg: {
	observedAt?: time.Time @protobuf(1,google.protobuf.Timestamp,name=observed_at)
}
