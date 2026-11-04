package kube

import (
	"context"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"periscope/internal/resource"
)

// The status of each kind, as docs/state-model.md#raw-dimensions lists it.
// Created is set by snapshot.add.

func namespaceStatus(n *corev1.Namespace) *resource.Status {
	return &resource.Status{Terminating: n.Status.Phase == corev1.NamespaceTerminating}
}

// replicas is spec.replicas, which defaults to 1.
func replicas(r *int32) int {
	if r == nil {
		return 1
	}
	return int(*r)
}

func deploymentStatus(d *appsv1.Deployment) *resource.Status {
	return &resource.Status{Desired: replicas(d.Spec.Replicas), Ready: int(d.Status.ReadyReplicas), Updated: int(d.Status.UpdatedReplicas)}
}

func statefulSetStatus(d *appsv1.StatefulSet) *resource.Status {
	return &resource.Status{Desired: replicas(d.Spec.Replicas), Ready: int(d.Status.ReadyReplicas), Updated: int(d.Status.UpdatedReplicas)}
}

func daemonSetStatus(d *appsv1.DaemonSet) *resource.Status {
	return &resource.Status{Desired: int(d.Status.DesiredNumberScheduled), Ready: int(d.Status.NumberReady), Updated: int(d.Status.UpdatedNumberScheduled)}
}

// jobStatus maps a Job's conditions to a phase, like a pod's: Failed is
// failed, Complete is completed. Its replica counts stay zero, since a Job
// whose pod hasn't finished yet is not down.
func jobStatus(j *batchv1.Job) *resource.Status {
	st := &resource.Status{Suspended: j.Spec.Suspend != nil && *j.Spec.Suspend}
	for _, c := range j.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobFailed:
			st.Phase = resource.PhaseFailed
		case batchv1.JobComplete:
			st.Phase = resource.PhaseSucceeded
		}
	}
	return st
}

func cronJobStatus(c *batchv1.CronJob) *resource.Status {
	return &resource.Status{Suspended: c.Spec.Suspend != nil && *c.Spec.Suspend}
}

// podStatus lists the containers in the order of the spec, with the image
// the spec names (the status may name a resolved digest).
func podStatus(p *corev1.Pod) *resource.Status {
	st := &resource.Status{Phase: resource.Phase(p.Status.Phase), Node: p.Spec.NodeName}
	for _, c := range p.Spec.Containers {
		ct := resource.Container{Name: c.Name, Image: c.Image}
		i := slices.IndexFunc(p.Status.ContainerStatuses, func(s corev1.ContainerStatus) bool { return s.Name == c.Name })
		if i >= 0 {
			cs := p.Status.ContainerStatuses[i]
			ct.Ready, ct.Restarts = cs.Ready, int(cs.RestartCount)
			switch {
			case cs.State.Running != nil:
				ct.State = resource.ContainerRunning
			case cs.State.Terminated != nil:
				ct.State, ct.Reason = resource.ContainerTerminated, cs.State.Terminated.Reason
			case cs.State.Waiting != nil:
				ct.Reason = cs.State.Waiting.Reason
			}
			if t := cs.LastTerminationState.Terminated; t != nil {
				ct.LastReason, ct.LastExit = t.Reason, t.FinishedAt.Time
			}
		}
		st.Containers = append(st.Containers, ct)
	}
	return st
}

// listEvents lists the warning events of the cluster (or of each namespace,
// if that is forbidden) and sorts them to the objects they are about, newest
// first. Events are an addition: if they can't be listed, objects have none.
func (p *Provider) listEvents(ctx context.Context, s *snapshot) map[string][]resource.Event {
	ctx, cancel := context.WithTimeout(ctx, eventsTimeout)
	defer cancel()
	evs, _ := listKind(ctx, s.nss, s.all, func(ns string, o metav1.ListOptions) ([]corev1.Event, string, error) {
		o.FieldSelector = "type=" + corev1.EventTypeWarning
		r, err := p.client.CoreV1().Events(ns).List(ctx, o)
		return items(r, err, func(r *corev1.EventList) ([]corev1.Event, string) { return r.Items, r.Continue })
	})
	out := map[string][]resource.Event{}
	for _, e := range evs {
		if e.Type != corev1.EventTypeWarning { // a server that ignores the field selector
			continue
		}
		path, ok := s.target(&e.InvolvedObject)
		if !ok {
			continue
		}
		out[path] = append(out[path], event(&e))
	}
	for _, evs := range out {
		slices.SortStableFunc(evs, func(a, b resource.Event) int { return b.Last.Compare(a.Last) })
	}
	return out
}

// target is the path of the object an event is about: by UID, so the events
// of an earlier pod of the same name (a StatefulSet's) don't count, else by
// name.
func (s *snapshot) target(r *corev1.ObjectReference) (string, bool) {
	if r.UID != "" {
		path, ok := s.byUID[r.UID]
		return path, ok
	}
	path, ok := s.byName[r.Kind+"/"+r.Namespace+"/"+r.Name]
	return path, ok
}

// event maps an event; series (the events.k8s.io way of counting) take
// precedence over the legacy count and time.
func event(e *corev1.Event) resource.Event {
	ev := resource.Event{Reason: e.Reason, Message: e.Message, Count: int(e.Count), Last: e.LastTimestamp.Time}
	if s := e.Series; s != nil {
		ev.Count = max(ev.Count, int(s.Count))
		if !s.LastObservedTime.IsZero() {
			ev.Last = s.LastObservedTime.Time
		}
	}
	for _, t := range []time.Time{e.EventTime.Time, e.FirstTimestamp.Time, e.CreationTimestamp.Time} {
		if !ev.Last.IsZero() {
			break
		}
		ev.Last = t
	}
	ev.Count = max(ev.Count, 1)
	return ev
}
