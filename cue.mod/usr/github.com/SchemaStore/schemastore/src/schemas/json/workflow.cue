package json

#job:  ((#Workflow & {jobs: _}).jobs & {x: _}).x
#step: ((#job & {steps: _}).steps & [_])[0]

// CUE does not properly encode a JSON Schema oneOf; see
// https://cuelang.org/issue/3165. For now, apply a temporary workaround which
// forces the other option to bottom.
#Workflow: jobs?: [string]: steps?: [...(
	{
		uses?: _|_
	} | {
		run?: _|_
	}),
]
