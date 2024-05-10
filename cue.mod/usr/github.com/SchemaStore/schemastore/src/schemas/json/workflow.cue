package json

#job:  ((#Workflow & {}).jobs & {x: _}).x
#step: ((#job & {steps: _}).steps & [_])[0]

// CUE does not properly encode a JSON Schema oneOf. This will be fixed in
// https://cuelang.org/issue/943, but for now we apply this patch.
#Workflow: jobs: [string]: steps: [...(
	{
		uses?: _|_
	} | {
		run?: _|_
	}),
]
