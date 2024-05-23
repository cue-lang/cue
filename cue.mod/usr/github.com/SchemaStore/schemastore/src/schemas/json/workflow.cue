package json

#job:  ((#Workflow & {}).jobs & {x: _}).x
#step: ((#job & {steps: _}).steps & [_])[0]

// CUE does not properly encode a JSON Schema oneOf. This will be fixed in
// https://cuelang.org/issue/943, but for now we apply this patch.
//
// See also the discussion in https://cuelang.org/issue/3165 on how oneofs
// could/should be encoded in CUE. https://cuelang.org/issue/943 suggests one
// approach, https://cuelang.org/issue/3165 is a more general exploration of
// the space.
#Workflow: jobs?: [string]: steps?: [...(
	{
		uses?: _|_
	} | {
		run?: _|_
	}),
]
