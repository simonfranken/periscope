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

A Deployment's pods belong to it through its ReplicaSets, which are not
shown: they are an implementation detail of a rollout. A Job created by a
CronJob is listed under the CronJob *(planned, with the provider)*.

## Raw dimensions

`Status` stores independent facts, like `kubectl get -o yaml` has them
(submarine D3):

| Field | For | From |
|---|---|---|
| `Created` | all | `metadata.creationTimestamp` |
| `Terminating` | namespaces | `status.phase == Terminating` |
| `Desired`, `Ready`, `Updated` | workloads | replicas (DaemonSet: `desiredNumberScheduled`, `numberReady`, `updatedNumberScheduled`) |
| `Suspended` | CronJob, Job | `spec.suspend` |
| `Phase`, `Node` | pods | `status.phase`, `spec.nodeName` |
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
| `pending` | Warn | a pod is in phase `Pending` |
| `not ready` | Warn | a running pod has a running container that isn't ready |
| `degraded` | Warn | a workload has some, but not all replicas ready |
| `restarting` | Warn | a container restarted within the last hour (and it wasn't an OOM kill) |
| `rolling out` | Info | a workload has fewer updated replicas than desired |
| `suspended` | Info | a CronJob or Job is suspended |
| `terminating` | Info | a namespace is being deleted |

- *Levels.* Danger: something is broken now. Warn: something isn't
  working as intended, or might break. Info: something is in progress or
  switched off on purpose. Quiet: `healthy` (`completed` for a pod in phase
  `Succeeded`). See submarine D4 and D26 for the three levels plus info.
- *Restarts are recent or quiet.* A pod that restarted twice three weeks ago
  is fine; a restart count alone would make long-running pods look broken.
  `RecentRestart` (1 h) is the window.
- *A workload scaled to zero is healthy.* `Desired == 0` raises nothing.
- *Problems of children do not mark the parent* (submarine D7): a
  Deployment with a crashing pod is `degraded` because of its own replica
  count, not because of the pod's issue. Collapsed parents count the issues
  below them (`HiddenIssues`), one count per level. An object whose status
  failed to load counts as danger below.

## Planned kinds

Kinds beyond workloads and pods (requirements F60–F67,
[P14](decisions.md#p14-resources-beyond-workloads)). The issue words and
levels are a proposal, to be confirmed when they are implemented.

| Kind | Parent | Group header | Issues | Muted fact when fine |
|---|---|---|---|---|
| Service | namespace | `services` | `no endpoints` (Warn): has a selector, no ready endpoint | type, `2 endpoints` |
| Ingress | namespace | `ingresses` | `backend missing` (Danger): a backend Service doesn't exist; `no address` (Warn): no load balancer address yet | hosts |
| PersistentVolumeClaim | namespace | `persistentvolumeclaims` | `lost` (Danger): its volume is gone; `pending` (Warn): not bound yet | size, storage class |
| ConfigMap | namespace | `configmaps` | none | `3 keys` |
| Secret | namespace | `secrets` | none | type, `3 keys` (values hidden, F64) |
| Node | cluster | `nodes` (top level) | `not ready` (Danger); `pressure` (Warn): memory, disk or PID pressure; `cordoned` (Info): unschedulable | roles, version |
| custom resource | namespace (or the top level if cluster-scoped) | `custom resources`, then its plural, e.g. `certificates`, alphabetically; only kinds with objects | `not ready` (Warn): `Ready` condition is `False`, with its reason | none |

- A Service with no endpoints because its workload is scaled to zero is
  quiet, like the workload.
- A kind without a notion of broken never shows `healthy` (F62): the muted
  fact says what is known instead.

## Cluster

The cluster's own status carries only `Created` for now. With the provider
the nodes become objects of their own (F66); their issues count below the
cluster like those of namespaces.
