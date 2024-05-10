package json

#job:  ((#Workflow & {}).jobs & {x: _}).x
#step: ((#job & {steps: _}).steps & [_])[0]

#Workflow: jobs: [string]: steps: [...(
	{
		uses?: _|_
	} | {
		run?: _|_
	}),
]
