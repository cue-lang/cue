package cuetxtar

import "cuelang.org/go/cue/stats"

// SignificantStatsChange reports whether the difference between original and new
// evaluation counts represents a significant change worth recording in golden files.
func SignificantStatsChange(orig, counts stats.Counts) bool {
	switch {
	case orig.Disjuncts < counts.Disjuncts,
		orig.Disjuncts > counts.Disjuncts*5 && counts.Disjuncts > 20,
		orig.Conjuncts > counts.Conjuncts*2,
		counts.Notifications > 10,
		counts.NumCloseIDs > 100,
		counts.MaxReqSets > 15,
		counts.Leaks()-orig.Leaks() > 17,
		counts.Allocs-orig.Allocs > 50:
		return true
	}
	return false
}
