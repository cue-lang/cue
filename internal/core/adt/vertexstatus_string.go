// Code generated by "stringer -type=vertexStatus"; DO NOT EDIT.

package adt

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[unprocessed-0]
	_ = x[evaluating-1]
	_ = x[partial-2]
	_ = x[conjuncts-3]
	_ = x[evaluatingArcs-4]
	_ = x[finalized-5]
}

const _vertexStatus_name = "unprocessedevaluatingpartialconjunctsevaluatingArcsfinalized"

var _vertexStatus_index = [...]uint8{0, 11, 21, 28, 37, 51, 60}

func (i vertexStatus) String() string {
	if i < 0 || i >= vertexStatus(len(_vertexStatus_index)-1) {
		return "vertexStatus(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _vertexStatus_name[_vertexStatus_index[i]:_vertexStatus_index[i+1]]
}