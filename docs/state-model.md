# State model

How periscope decides what an object's state is, and which states are
problems. The rules live in `internal/resource` (`Status.Issues()`); the
words and colors in `internal/tui/theme`.

## Objects

| Kind | Parent | Path (unique ID) | Group header |
|---|---|---|---|
| cluster | none | `""` | none; shown by the header |
| namespace | cluster | `shop` | none |
| Deployment, StatefulSet, DaemonSet, Job, CronJob | namespace | `shop/deployment/api` | `deployments`, … |
| pod | its workload | `shop/deployment/api/pod/api-7b8d9f6c5-zp6qx` | none |
| Service, Ingress, Route | namespace | `shop/service/web`, `shop/ingress/shop`, `shop/route/web` | `services`, `ingresses`, `routes` |
| ConfigMap, Secret | namespace | `shop/configmap/api-config`, `shop/secret/api-tls` | `configmaps`, `secrets` |
| section `custom resources` | namespace, or the top level | `shop/custom resources`, `custom resources` | none |
| custom resource | the section of its namespace | `shop/certificate.cert-manager.io/api-tls` | its plural: `certificates` |
| custom resource, cluster-scoped | the top-level section | `clusterissuer.cert-manager.io/letsencrypt` | as above |

A path is the namespace's path, `/`, and the object's kubectl ref (`Ref`:
`deployment/api`, `certificate.cert-manager.io/api-tls`); a pod's adds its
own to its workload's. A custom resource's path doesn't name its section,
which is a tree node only. `resource.Arrange` puts every provider's tree
into display order, moves the custom resources into their sections and
sets their headers (F67). A section has no state of its own: it gets an
empty `Status`, is never loaded, and shows no state word.

A Deployment's pods belong to it through its ReplicaSets, which are not
shown: they are an implementation detail of a rollout. A Job created by a
CronJob is listed under the CronJob (`shop/cronjob/report/job/report-1`,
no group header), its pods below it. A pod without a workload is a child
of its namespace under `pods` (`shop/pod/debug`, F3). Ownership is the
controller owner reference.

Services, Ingresses, Routes (OpenShift's `route.openshift.io`, a kind of
its own, [P21](decisions.md#p21-services-ingresses-routes-configmaps-and-secrets)),
ConfigMaps and Secrets are children of their namespace, in that order
around the `custom resources` section (F60). What a Service selects and
what an Ingress routes to are facts in their status, not tree levels
(F63).

## Raw dimensions

`Status` stores independent facts, like `kubectl get -o yaml` has them
(submarine D3):

| Field | For | From |
|---|---|---|
| `Created` | all | `metadata.creationTimestamp` |
| `Terminating` | namespaces | `status.phase == Terminating` |
| `Desired`, `Ready`, `Updated` | workloads | replicas (DaemonSet: `desiredNumberScheduled`, `numberReady`, `updatedNumberScheduled`) |
| `Suspended` | CronJob, Job | `spec.suspend` |
| `Phase` | Job | `Failed` if its condition `Failed` is true, `Succeeded` if `Complete` is; its replica fields stay zero, so a running Job is not `down` |
| `Phase`, `Node` | pods | `status.phase`, `spec.nodeName` |
| `Type`, `Selector` | Services | `spec.type` (default `ClusterIP`); `spec.selector` as `app=web`, empty for none or an `ExternalName` Service |
| `Endpoints` | Services | ready endpoints of its EndpointSlices (`ready` unset counts as ready), a pod once though dual-stack lists it twice; where EndpointSlices can't be listed, the ready pods (`Ready` condition) its selector matches |
| `Idle`, `Targets` | Services | `Targets`: the workloads whose pod template the selector matches, and the tree's parent of each pod it matches (a workload, or the bare pod), as refs. `Idle`: no ready endpoint, and every workload it matches wants 0 replicas |
| `Hosts`, `Addresses` | Ingresses, Routes | Ingress: `rules[].host` (`*` for a rule without one, or for a default backend only), `status.loadBalancer.ingress[]` IP or host name. Route: `spec.host`, the `routerCanonicalHostname` (else `routerName`) of each router whose `Admitted` condition is `True` |
| `Backends` | Ingresses, Routes | the Services of `defaultBackend` and `rules[].http.paths[].backend` (a `resource` backend is none), of `spec.to` and `spec.alternateBackends`; `Missing` if the namespace has no such Service, never where Services can't be listed |
| `Rejected` | Routes | the reason of an `Admitted` condition `False`, if no router admitted it |
| `Type`, `Keys` | ConfigMaps, Secrets | `type` (Secrets); the keys of `data` and `binaryData` and the size of each value. Values are dropped as each page is listed (F64) |
| `Conditions` | custom resources only | `status.conditions`: type, status, reason, message. Not read for pods and workloads, whose state is in the fields above |
| `Containers` | pods | `status.containerStatuses`: ready, state, reason, restarts, last termination reason and time |
| `Events` | all | warning events whose `involvedObject` is the object, newest first |

## Issues

`Status.Issues()` turns the dimensions into issues, most severe first. Each
kind appears at most once; an issue about a container names it.

| Issue | Level | When |
|---|---|---|
| `down` | Danger | a workload wants replicas and none is ready |
| `crashloop` | Danger | a container waits in `CrashLoopBackOff` |
| `oom killed` | Danger | a container was `OOMKilled` within the last hour |
| `pull error` | Danger | a container waits in `ImagePullBackOff` or `ErrImagePull` |
| `failed` | Danger | a pod (or Job) is in phase `Failed` |
| `backend missing` | Danger | an Ingress or Route routes to a Service that doesn't exist |
| `pending` | Warn | a pod is in phase `Pending` |
| `not ready` | Warn | a running pod has a running container that isn't ready; a custom resource's `Ready` condition is `False` |
| `degraded` | Warn | a workload has some, but not all replicas ready |
| `no endpoints` | Warn | a Service has a selector and no ready endpoint, and isn't `Idle` |
| `no address` | Warn | an Ingress has no load balancer address; no router admitted a Route (or one rejected it) |
| `restarting` | Warn | a container restarted within the last hour (and it wasn't an OOM kill) |
| `rolling out` | Info | a workload has fewer updated replicas than desired |
| `suspended` | Info | a CronJob or Job is suspended |
| `terminating` | Info | a namespace is being deleted |

- *Levels.* Danger: something is broken now. Warn: something isn't
  working as intended, or might break. Info: something is in progress or
  switched off on purpose. Quiet: `healthy` (`unchecked` for a custom
  resource without a Ready condition, `completed` for a pod in phase
  `Succeeded`, a fact for the kinds below). See submarine D4 and D26 for the three levels plus info.
- *Restarts are recent or quiet.* A pod that restarted twice three weeks ago
  is fine; a restart count alone would make long-running pods look broken.
  `RecentRestart` (1 h) is the window.
- *A workload scaled to zero is healthy.* `Desired == 0` raises nothing,
  and neither does a Service of it (`Idle`).
- *Facts instead of healthy* (F62). Services, Ingresses and Routes
  without issues, and ConfigMaps and Secrets always, show a muted fact
  instead of `healthy`: `2 endpoints` (`no selector` for a Service without
  one, `external name` for an `ExternalName` Service), the first host with
  `+n` for more, `3 keys`. ConfigMaps and Secrets have no notion of
  broken, so `healthy` would claim a check that never ran.
- *Endpoints, not pods.* The endpoints are what traffic reaches: a Service
  whose `targetPort` names a port the pods don't have has ready pods but
  no endpoints, which counting pods would miss. Pods count only where
  EndpointSlices can't be listed. An Ingress controller that never writes
  its address into the status leaves every Ingress `no address`.
- *Problems of children do not mark the parent* (submarine D7): a
  Deployment with a crashing pod is `degraded` because of its own replica
  count, not because of the pod's issue. Collapsed parents count the issues
  below them (`HiddenIssues`), one count per level. An object whose status
  failed to load counts as danger below.

## Planned kinds

Kinds still to come (requirements F60, F66,
[P14](decisions.md#p14-resources-beyond-workloads)). The issue words and
levels are a proposal, to be confirmed when they are implemented.

| Kind | Parent | Group header | Issues | Muted fact when fine |
|---|---|---|---|---|
| PersistentVolumeClaim | namespace | `persistentvolumeclaims` | `lost` (Danger): its volume is gone; `pending` (Warn): not bound yet | size, storage class |
| Node | cluster | `nodes` (top level) | `not ready` (Danger); `pressure` (Warn): memory, disk or PID pressure; `cordoned` (Info): unschedulable | roles, version |

## Cluster

The cluster's own status carries only `Created` for now. With the provider
the nodes become objects of their own (F66); their issues count below the
cluster like those of namespaces.
