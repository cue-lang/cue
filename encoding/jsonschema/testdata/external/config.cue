@extern(embed)

package external

// TODO use recursive globbing when it's available.

allTests: _	@embed(glob=tests/*/*.json)
allTests: _	@embed(glob=tests/*/*/*.json)
allTests: _	@embed(glob=tests/*/*/*/*.json)

allTests: [_]: [... #Schema]
#Schema: {
	description!: string
	comment?: string
	specification?: _
	schema!: _
	tests!: [... #Test]

	// skip is not part of the orginal test data, but
	// is inserted by our test logic (when CUE_UPDATE=1)
	// to indicate which tests are passing and which
	// are failing. The text indicates some reason as to
	// why the schema is skipped.
	skip?: string
}

#Test: {
	description!: string
	comment?: string
	data!: _
	valid!: bool

	// skip is not part of the orginal test data, but
	// is inserted by our test logic (when CUE_UPDATE=1)
	// to indicate which tests are passing and which
	// are failing. The text indicates some reason as to
	// why the test is skipped.
	skip?: string
}
