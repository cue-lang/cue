package kube

import (
  "k8s.io/api/core/v1"
  extensions_v1beta1 "k8s.io/api/extensions/v1beta1"
  apps_v1beta1 "k8s.io/api/apps/v1beta1"
)

service: [string]:     v1.Service
deployment: [string]:  extensions_v1beta1.Deployment
daemonSet: [string]:   extensions_v1beta1.DaemonSet
statefulSet: [string]: apps_v1beta1.StatefulSet
