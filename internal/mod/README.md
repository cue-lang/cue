## WIP Modules FAQ/README/etc

This is a WIP file in which we capture aspects of the modules implementation
that need documenting. Some take the form of FAQ-like questions, others are
simply details which we need to ensure are documented.  Some of the points
listed below could be part of a modules reference doc, others a modules FAQ...
We don't seek to make a decision on what docs/FAQs etc should exist, just use
this opportunity to capture the bits that need coverage.

Some of the questions below are presented with answers, many are just left here
as a TODO.

The goal of this doc is to capture things in a central VCS-based file, alongside
the code that "raises" the question/similar.

* Documentation around defaulting the value of `CUE_REGISTRY` (or whatever we
  call it) to the central registry.
* Document that `pkg`, `usr` and `gen` directories cannot co-exist with modules,
  and make clear what users (especially of `gen`) should probably do instead.
* Document potential future vendor behaviour.
* Document what we mean by a "canonical" version, how this differs from a
  "well-formed" version. Present examples of each.
