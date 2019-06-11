package kube

import (
  "k8s.io/api/core/v1"
  extensions_v1beta1 "k8s.io/api/extensions/v1beta1"
  apps_v1beta1 "k8s.io/api/apps/v1beta1"
)

service <Name>: v1.Service & {}
deployment <Name>: extensions_v1beta1.Deployment & {}
daemonSet <Name>: extensions_v1beta1.DaemonSet & {}
statefulSet <Name>: apps_v1beta1.StatefulSet & {}
