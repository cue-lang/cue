package json

#job:  ((#Workflow & {jobs: _}).jobs & {x: _}).x
#step: ((#job & {steps: _}).steps & [_])[0]
