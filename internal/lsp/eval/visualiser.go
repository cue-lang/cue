package eval

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

type Visualiser struct {
	e                *Evaluator
	snapshotCounter  int
	maxColumnCounter int
	navigables       map[*navigable]*navigableVis
	frames           map[*frame]*frameVis
}

func NewVisualiser(e *Evaluator) *Visualiser {
	vis := &Visualiser{
		e:          e,
		navigables: make(map[*navigable]*navigableVis),
		frames:     make(map[*frame]*frameVis),
	}
	e.visualiser = vis
	return vis
}

func (v *Visualiser) Snapshot() {
	snapshotCounter := v.snapshotCounter
	v.snapshotCounter++

	v.snapshotFrames(snapshotCounter)
	v.snapshotNavigables(snapshotCounter)
}

func (v *Visualiser) snapshotNavigables(snapshotCounter int) {
	processedNavs := make(map[*navigable]struct{})
	navigables := v.navigables

	worklist := []*navigable{}
	worklistNext := []*navigable{v.e.pkgFrame.navigable}
	columnCounter := -1
	needsEdges := make(map[*snapshotPair]*navigableVis)
	for len(worklistNext) > 0 {
		worklist, worklistNext = worklistNext, worklist
		worklistNext = worklistNext[:0]

		columnCounter++
		columnId := fmt.Sprintf("column nav %d", columnCounter)

		for worklist := worklist; len(worklist) > 0; {
			nav := worklist[0]
			worklist = worklist[1:]
			if _, seen := processedNavs[nav]; seen {
				continue
			}
			processedNavs[nav] = struct{}{}

			worklistNext = slices.AppendSeq(worklistNext, maps.Values(nav.bindings))
			worklistNext = slices.AppendSeq(worklistNext, maps.Keys(nav.resolvesTo))
			for _, fr := range nav.frames {
				for _, childFr := range fr.childFrames {
					worklistNext = append(worklistNext, childFr.navigable)
				}
			}

			navVis, found := navigables[nav]
			if !found {
				navVis = &navigableVis{
					columnId:  columnId,
					navigable: nav,
				}
				navigables[nav] = navVis
			}
			navSnap := newNavigableSnapshotNode(navVis)
			if l := len(navVis.snapshots); l == 0 || navVis.snapshots[l-1].snapshot.node != navSnap.node {
				navSnapPair := &snapshotPair{
					snapshotCounter: snapshotCounter,
					snapshot:        navSnap,
				}
				navVis.snapshots = append(navVis.snapshots, navSnapPair)
				needsEdges[navSnapPair] = navVis
			}
		}
	}

	for navSnapPair, navVis := range needsEdges {
		navSnapPair.snapshot.addNavigableEdges(v, navVis)
	}

	v.maxColumnCounter = max(v.maxColumnCounter, columnCounter+1)
}

func (v *Visualiser) snapshotFrames(snapshotCounter int) {
	processedFrames := make(map[*frame]struct{})
	frames := v.frames

	worklist := []*frame{}
	worklistNext := []*frame{v.e.pkgFrame}
	columnCounter := -1
	needsEdges := make(map[*snapshotPair]*frameVis)
	for len(worklistNext) > 0 {
		worklist, worklistNext = worklistNext, worklist
		worklistNext = worklistNext[:0]

		columnCounter++
		columnId := fmt.Sprintf("column fr %d", columnCounter)

		for worklist := worklist; len(worklist) > 0; {
			fr := worklist[0]
			worklist = worklist[1:]
			if _, seen := processedFrames[fr]; seen {
				continue
			}
			processedFrames[fr] = struct{}{}

			worklistNext = append(worklistNext, fr.childFrames...)

			frVis, found := frames[fr]
			if !found {
				frVis = &frameVis{
					columnId: columnId,
					frame:    fr,
				}
				frames[fr] = frVis
			}
			frSnap := newFrameSnapshotNode(frVis)
			if l := len(frVis.snapshots); l == 0 || frVis.snapshots[l-1].snapshot.node != frSnap.node {
				frSnapPair := &snapshotPair{
					snapshotCounter: snapshotCounter,
					snapshot:        frSnap,
				}
				frVis.snapshots = append(frVis.snapshots, frSnapPair)
				needsEdges[frSnapPair] = frVis
			}
		}
	}

	for frSnapPair, frVis := range needsEdges {
		frSnapPair.snapshot.addFrameEdges(v, frVis)
	}

	v.maxColumnCounter = max(v.maxColumnCounter, columnCounter+1)
}

func (v *Visualiser) Render() string {
	navigables := v.navigables
	frames := v.frames
	sb := new(strings.Builder)

	// navigables
	// start by rendering all the nodes
	for _, navVis := range navigables {
		l := len(navVis.snapshots)
		if l == 0 {
			continue
		}
		navVisSnap := navVis.snapshots[l-1]
		sb.WriteString(navVisSnap.snapshot.node)
	}
	// then all the edges
	for _, navVis := range navigables {
		l := len(navVis.snapshots)
		if l == 0 {
			continue
		}
		navVisSnap := navVis.snapshots[l-1]
		sb.WriteString(navVisSnap.snapshot.edges)
	}
	// now frames
	for _, frVis := range frames {
		l := len(frVis.snapshots)
		if l == 0 {
			continue
		}
		frVisSnap := frVis.snapshots[l-1]
		sb.WriteString(frVisSnap.snapshot.node)
	}
	for _, frVis := range frames {
		l := len(frVis.snapshots)
		if l == 0 {
			continue
		}
		frVisSnap := frVis.snapshots[l-1]
		sb.WriteString(frVisSnap.snapshot.edges)
	}

	sb.WriteString("\nscenarios: {\n")
	for snapshotCounter := range v.snapshotCounter {
		fmt.Fprintf(sb, "  snapshot %d: {\n", snapshotCounter)

		for _, navVis := range navigables {
			snapshotPairs := navVis.snapshots
			for i, snapshotPair := range snapshotPairs {
				if snapshotPair.snapshotCounter <= snapshotCounter && (i+1 == len(snapshotPairs) || snapshotCounter < snapshotPairs[i+1].snapshotCounter) {
					sb.WriteString(snapshotPair.snapshot.node)
					if snapshotPair.snapshotCounter == snapshotCounter {
						fmt.Fprintf(sb, `
%s.nav %p.style.opacity: %f
%s.nav %p.**.style.opacity: %f
`[1:], navVis.columnId, navVis.navigable, fullOpacity, navVis.columnId, navVis.navigable, fullOpacity)
						sb.WriteString(snapshotPair.snapshot.edgesEnableNew)
					} else {
						fmt.Fprintf(sb, `
%s.nav %p.style.opacity: %f
%s.nav %p.**.style.opacity: %f
`[1:], navVis.columnId, navVis.navigable, dimOpacity, navVis.columnId, navVis.navigable, dimOpacity)
						sb.WriteString(snapshotPair.snapshot.edgesEnableOld)
					}
					break
				}
			}
		}

		for _, frVis := range frames {
			snapshotPairs := frVis.snapshots
			for i, snapshotPair := range snapshotPairs {
				if snapshotPair.snapshotCounter <= snapshotCounter && (i+1 == len(snapshotPairs) || snapshotCounter < snapshotPairs[i+1].snapshotCounter) {
					sb.WriteString(snapshotPair.snapshot.node)
					if snapshotPair.snapshotCounter == snapshotCounter {
						fmt.Fprintf(sb, `
%s.fr %p.style.opacity: %f
%s.fr %p.**.style.opacity: %f
`[1:], frVis.columnId, frVis.frame, fullOpacity, frVis.columnId, frVis.frame, fullOpacity)
						sb.WriteString(snapshotPair.snapshot.edgesEnableNew)
					} else {
						fmt.Fprintf(sb, `
%s.fr %p.style.opacity: %f
%s.fr %p.**.style.opacity: %f
`[1:], frVis.columnId, frVis.frame, dimOpacity, frVis.columnId, frVis.frame, dimOpacity)
						sb.WriteString(snapshotPair.snapshot.edgesEnableOld)
					}
					break
				}
			}
		}
		fmt.Fprint(sb, "  }\n")
	}
	sb.WriteString("}\n")

	return frontmatter + sb.String()
}

type navigableVis struct {
	columnId  string
	navigable *navigable
	snapshots []*snapshotPair
}

type frameVis struct {
	columnId  string
	frame     *frame
	snapshots []*snapshotPair
}

type snapshotPair struct {
	snapshotCounter int
	snapshot        nodeEdges
}

type nodeEdges struct {
	node           string
	edges          string
	edgesEnableNew string
	edgesEnableOld string
}

func newNavigableSnapshotNode(navVis *navigableVis) nodeEdges {
	nav := navVis.navigable
	navColId := navVis.columnId
	navId := fmt.Sprintf("nav %p", nav)

	name := ""
	if nav.name != "" {
		name = nav.name + " " + navId
	}
	node := fmt.Sprintf(`
%s.%s: %s {
    class: nav
    evaluated: evaluated: %v {class: field}
    bindings: bindings: %d {class: field}
    resolvesTo: resolvesTo: %d {class: field}
    frames: frames: %d {class: field}
}
`, navColId, navId, name, nav.evaluated, len(nav.bindings), len(nav.resolvesTo), len(nav.frames))

	return nodeEdges{
		node: node,
	}
}

func (ne *nodeEdges) addNavigableEdges(v *Visualiser, navVis *navigableVis) {
	navigables := v.navigables
	nav := navVis.navigable
	navColId := navVis.columnId
	navId := fmt.Sprintf("nav %p", nav)

	// 1. Bindings
	names := make([]string, 0, len(nav.bindings))
	for name := range nav.bindings {
		names = append(names, name)
	}
	slices.Sort(names)

	edgesSb := new(strings.Builder)
	edgesEnableNewSb := new(strings.Builder)
	edgesEnableOldSb := new(strings.Builder)
	for _, name := range names {
		binding := nav.bindings[name]
		bindingVis, found := navigables[binding]
		if !found { // it's been filtered out for some reason
			continue
		}
		bindingId := fmt.Sprintf("nav %p", binding)

		fmt.Fprintf(edgesSb, `
%s.%s.bindings -> %s.%s: %s {
  class: bindingEdge
}
`, navColId, navId, bindingVis.columnId, bindingId, name)

		fmt.Fprintf(edgesEnableNewSb, "(%s.%s.bindings -> %s.%s)[0].style.opacity: %f\n", navColId, navId, bindingVis.columnId, bindingId, fullOpacity)
		fmt.Fprintf(edgesEnableOldSb, "(%s.%s.bindings -> %s.%s)[0].style.opacity: %f\n", navColId, navId, bindingVis.columnId, bindingId, dimOpacity)
	}

	// 2. ResolvesTo
	for resolvesTo := range nav.resolvesTo {
		resolvesToVis, found := navigables[resolvesTo]
		if !found { // it's been filtered out for some reason
			continue
		}
		resolvesToId := fmt.Sprintf("nav %p", resolvesTo)

		fmt.Fprintf(edgesSb, `
%s.%s.resolvesTo -> %s.%s: resolves to {
  class: resolvesToEdge
}
`, navColId, navId, resolvesToVis.columnId, resolvesToId)

		fmt.Fprintf(edgesEnableNewSb, "(%s.%s.resolvesTo -> %s.%s)[0].style.opacity: %f\n", navColId, navId, resolvesToVis.columnId, resolvesToId, fullOpacity)
		fmt.Fprintf(edgesEnableOldSb, "(%s.%s.resolvesTo -> %s.%s)[0].style.opacity: %f\n", navColId, navId, resolvesToVis.columnId, resolvesToId, dimOpacity)
	}

	// 3. Frames
	frames := v.frames
	for _, fr := range nav.frames {
		frVis, found := frames[fr]
		if !found { // it's been filtered out for some reason
			continue
		}
		frId := fmt.Sprintf("fr %p", fr)

		fmt.Fprintf(edgesSb, `
%s.%s.frames <-> %s.%s: frame {
  class: navFrameEdge
}
`, navColId, navId, frVis.columnId, frId)

		fmt.Fprintf(edgesEnableNewSb, "(%s.%s.frames <-> %s.%s)[0].style.opacity: %f\n", navColId, navId, frVis.columnId, frId, fullOpacity)
		fmt.Fprintf(edgesEnableOldSb, "(%s.%s.frames <-> %s.%s)[0].style.opacity: %f\n", navColId, navId, frVis.columnId, frId, dimOpacity)
	}

	ne.edges = edgesSb.String()
	ne.edgesEnableNew = edgesEnableNewSb.String()
	ne.edgesEnableOld = edgesEnableOldSb.String()
}

func newFrameSnapshotNode(frVis *frameVis) nodeEdges {
	fr := frVis.frame
	frColId := frVis.columnId
	frId := fmt.Sprintf("fr %p", fr)

	node := fmt.Sprintf(`
%s.%s: %s {
    class: fr
    evaluated: evaluated: %v {class: field}
    bindings: bindings: %d {class: field}
    childFrames: childFrames: %d {class: field}
}
`, frColId, frId, frId, fr.evaluated, len(fr.bindings), len(fr.childFrames))

	return nodeEdges{
		node: node,
	}
}

func (ne *nodeEdges) addFrameEdges(v *Visualiser, frVis *frameVis) {
	frames := v.frames
	fr := frVis.frame
	frColId := frVis.columnId
	frId := fmt.Sprintf("fr %p", fr)

	// 1. Bindings
	names := make([]string, 0, len(fr.bindings))
	for name := range fr.bindings {
		names = append(names, name)
	}
	slices.Sort(names)

	edgesSb := new(strings.Builder)
	edgesEnableNewSb := new(strings.Builder)
	edgesEnableOldSb := new(strings.Builder)
	for _, name := range names {
		for _, binding := range fr.bindings[name] {
			bindingVis, found := frames[binding]
			if !found { // it's been filtered out for some reason
				continue
			}
			bindingId := fmt.Sprintf("fr %p", binding)

			fmt.Fprintf(edgesSb, `
%s.%s.bindings -> %s.%s: %s {
  class: frameEdge
}
`, frColId, frId, bindingVis.columnId, bindingId, name)

			fmt.Fprintf(edgesEnableNewSb, "(%s.%s.bindings -> %s.%s)[0].style.opacity: %f\n", frColId, frId, bindingVis.columnId, bindingId, fullOpacity)
			fmt.Fprintf(edgesEnableOldSb, "(%s.%s.bindings -> %s.%s)[0].style.opacity: %f\n", frColId, frId, bindingVis.columnId, bindingId, dimOpacity)
		}
	}

	// 2. child frames
	for _, childFr := range fr.childFrames {
		childFrVis, found := frames[childFr]
		if !found { // it's been filtered out for some reason
			continue
		}
		childFrId := fmt.Sprintf("fr %p", childFr)

		fmt.Fprintf(edgesSb, `
%s.%s.childFrames <-> %s.%s: child frame {
  class: frameEdge
}
`, frColId, frId, childFrVis.columnId, childFrId)

		fmt.Fprintf(edgesEnableNewSb, "(%s.%s.childFrames <-> %s.%s)[0].style.opacity: %f\n", frColId, frId, childFrVis.columnId, childFrId, fullOpacity)
		fmt.Fprintf(edgesEnableOldSb, "(%s.%s.childFrames <-> %s.%s)[0].style.opacity: %f\n", frColId, frId, childFrVis.columnId, childFrId, dimOpacity)
	}

	ne.edges = edgesSb.String()
	ne.edgesEnableNew = edgesEnableNewSb.String()
	ne.edgesEnableOld = edgesEnableOldSb.String()
}

const frontmatter = `
vars: {
  d2-config: {
    layout-engine: elk
  }
}
direction: right

classes: {
  col: {
    style.stroke-width: 0
    style.opacity: 0.1
  }
  nav: {
    shape: rectangle
    style.stroke: "#500000"
    label.near: border-top-center
  }
  fr: {
    shape: rectangle
    style.stroke: "#005000"
    label.near: border-top-center
  }
  field: {
    shape: text
  }
  bindingEdge: {
    style.stroke: "#700000"
    style.stroke-width: 4
    style.font-size: 16
  }
  resolvesToEdge: {
    style.stroke: "#900000"
    style.stroke-width: 6
  }
  navFrameEdge: {
    style.stroke: "#500050"
    style.stroke-width: 2
  }
  frameEdge: {
    style.stroke: "#505000"
    style.stroke-width: 2
  }
}

*: {
  class: col
  label: ""
}
**: {
  &class: nav
  style.opacity: 0
}
**: {
  &class: fr
  style.opacity: 0
}
**: {
  &class: field
  style.opacity: 0
}
(** -> **)[*]: { style.opacity: 0 }
(** <-> **)[*]: { style.opacity: 0 }
`

const fullOpacity = 1.0
const dimOpacity = 0.6
