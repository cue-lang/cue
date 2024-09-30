#!/bin/env bash

here=`cd $(dirname $BASH_SOURCE); pwd`

export ANDUIN_CUE_DEBUG="true"
export CUE_CACHE_DIR=$here/cue.cache/
export CUE_REGISTRY=file://$here/registry.cue

$here/../../../dist/cue-dev $@
