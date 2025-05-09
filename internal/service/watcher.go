package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/slack-go/slack"
	v2 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	configv1alpha1 "github.com/mgufrone/pod-notifier/api/config/v1alpha1"
	"github.com/mgufrone/pod-notifier/internal/view"
)

const (
	ContainerResolved = "Resolved"
)

type Watcher struct {
	client.Client
	Scheme      *runtime.Scheme
	slackClient *slack.Client
}

func NewWatcher(client client.Client, scheme *runtime.Scheme, slackClient *slack.Client) *Watcher {
	return &Watcher{
		Client:      client,
		Scheme:      scheme,
		slackClient: slackClient,
	}
}

func (w *Watcher) Copy() *Watcher {
	return &Watcher{
		Client:      w.Client,
		Scheme:      w.Scheme,
		slackClient: w.slackClient,
	}
}

func (w *Watcher) getOwner(ctx context.Context, pod *v1.Pod) (*v1.ObjectReference, error) {
	podLog := log.FromContext(ctx).WithName("pod_owner")
	ownerRefs := pod.GetOwnerReferences()
	var owner *v1.ObjectReference
	for _, ownerRef := range ownerRefs {
		if ownerRef.Controller != nil && *ownerRef.Controller {
			owner = &v1.ObjectReference{
				Kind:       ownerRef.Kind,
				Name:       ownerRef.Name,
				Namespace:  pod.Namespace,
				APIVersion: ownerRef.APIVersion,
			}
			if owner.Kind == "ReplicaSet" {
				replicaSet := &v2.ReplicaSet{}
				if err := w.Get(ctx, client.ObjectKey{Namespace: owner.Namespace, Name: owner.Name}, replicaSet); err != nil {
					return nil, err
				}
				// Get the actual owner (Deployment or StatefulSet) from the ReplicaSet
				for _, rsOwnerRef := range replicaSet.OwnerReferences {
					if rsOwnerRef.Controller != nil && *rsOwnerRef.Controller {
						owner = &v1.ObjectReference{
							Kind:       rsOwnerRef.Kind,
							Name:       rsOwnerRef.Name,
							Namespace:  replicaSet.Namespace,
							APIVersion: rsOwnerRef.APIVersion,
						}
						break
					}
				}
			}
			podLog.V(8).Info("found pod owner", "ownerKind", owner.Kind, "ownerName", owner.Name)
			break
		}
	}
	return owner, nil
}
func (w *Watcher) Reconcile(ctx context.Context, podWatch configv1alpha1.PodWatchSpec, lastReports []configv1alpha1.PodReport, namespace string) ([]configv1alpha1.PodReport, error) {

	logger := log.FromContext(ctx).WithName("watcher")
	var (
		podList      v1.PodList
		mappedReport = map[string]configv1alpha1.PodReport{}
		resolvedPods = map[string]bool{}
		res          = make([]configv1alpha1.PodReport, 0)
		opts         []client.ListOption
	)
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	for _, report := range lastReports {
		mappedReport[report.Hash] = report
		resolvedPods[report.Hash] = true
		logger.V(8).Info("last pod hash", "hash", report.Hash)
	}
	if err := w.List(ctx, &podList, opts...); err != nil {
		logger.Error(err, "unable to fetch pods")
		return nil, err
	}
	for _, pod := range podList.Items {
		// identify the pod owner/controller
		var (
			failingReason string
			failingStatus string
		)
		podLog := logger.WithValues("pod", pod.GetName())
		// Get the owner references of the pod
		owner, err := w.getOwner(ctx, &pod)
		if err != nil {
			podLog.Error(err, "failed to fetch pod owner")
		}
		podKey := fmt.Sprintf("%s:%s", pod.Namespace, pod.Name)
		hashedKey := fmt.Sprintf("%x", sha256.Sum256([]byte(podKey)))
		report := configv1alpha1.PodReport{
			Name: pod.GetName(),
			Hash: hashedKey,
		}
		if owner != nil {
			report.OwnerRef = fmt.Sprintf("%s:%s", owner.Namespace, owner.Name)
		}

		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				failingReason = cs.State.Waiting.Message
				failingStatus = cs.State.Waiting.Reason
				break
			}

			// OOMKilled
			if cs.State.Terminated != nil && cs.State.Terminated.Reason == "OOMKilled" {
				failingReason = "Container is out of resources"
				failingStatus = cs.State.Terminated.Reason
				break
			}
		}
		if failingReason == "" && (failingStatus == "" || failingStatus == "ContainerCreating") {
			failingStatus, failingReason = w.determineFailingStatusFromEvent(ctx, pod, podLog)
		}

		// check if it's dangling terminated pods
		if failingStatus == "" && failingReason == "" && (pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed) {
			if !pod.GetObjectMeta().GetDeletionTimestamp().IsZero() &&
				pod.GetObjectMeta().GetFinalizers() != nil &&
				len(pod.GetObjectMeta().GetFinalizers()) > 0 {
				failingReason = "Pod is stuck at being deleted"
				failingStatus = "Dangling"
			}
		}
		// there's no reason to continue. so carry on
		report.LastStatus = failingStatus
		report.Reason = failingReason
		report.LastUpdated = time.Now().Format(time.RFC3339)
		resolvedPods[hashedKey] = false
		if failingReason == "" && failingStatus == "" {
			report.LastStatus = ContainerResolved
			resolvedPods[hashedKey] = true
		}
		// find out the reason status in this order: container status, events
		// when the container status doesn't find any error, go to events and see if any warning served
		// otherwise, all good. either mark the status as resolved or do not notify at all
		if err := w.processReport(mappedReport, podWatch, namespace, report); err != nil {
			podLog.Error(err, "unable to process report")
		}
	}
	for hash, resolved := range resolvedPods {
		if resolved {
			_ = w.processReport(mappedReport, podWatch, namespace, configv1alpha1.PodReport{
				Hash:        hash,
				LastStatus:  ContainerResolved,
				LastUpdated: time.Now().Format(time.RFC3339),
				Reason:      "",
			})
		}
	}

	for hash, report := range mappedReport {
		res = append(res, report)
		delete(mappedReport, hash)
	}
	return res, nil
}

func (w *Watcher) determineFailingStatusFromEvent(ctx context.Context, pod v1.Pod, podLog logr.Logger) (string, string) {
	var (
		failingReason string
		failingStatus string
	)
	var (
		eventList          v1.EventList
		eventFilterOptions = []client.ListOption{
			client.MatchingFields{
				"involvedObject.name":      pod.Name,
				"involvedObject.namespace": pod.Namespace,
				"type":                     v1.EventTypeWarning,
			},
		}
	)
	if err := w.List(ctx, &eventList, eventFilterOptions...); err != nil {
		podLog.Error(err, "unable to fetch events for pod")
	}

	sort.Slice(eventList.Items, func(i, j int) bool {
		return eventList.Items[i].LastTimestamp.Time.Before(eventList.Items[j].LastTimestamp.Time)
	})
	for _, es := range eventList.Items {
		podLog.Info(fmt.Sprintf("reason: %s; type: %s; desc: %s", es.Reason, es.Type, es.Message))
		if es.Reason == "FailedMount" || es.Reason == "Unhealthy" {
			failingStatus = es.Reason
			failingReason = es.Message
			break
		}
	}
	return failingStatus, failingReason
}

func (w *Watcher) processReport(mapReports map[string]configv1alpha1.PodReport, channel configv1alpha1.PodWatchSpec, namespace string, entry configv1alpha1.PodReport) error {
	logger := log.FromContext(context.Background()).WithName("slack_notifier")
	var ts, ch string
	writer := bytes.NewBuffer(nil)
	if err := view.Thread(view.ThreadData{
		Pod:       entry.Name,
		Namespace: namespace,
		Owner:     entry.OwnerRef,
		Reason:    entry.Reason,
		Status:    entry.LastStatus,
	}, writer); err != nil {
		return err
	}
	ch = channel.Channel
	_, ok := mapReports[entry.Hash]
	logger.V(8).Info("last pod hash", "hash", entry.Hash, "ok", ok, "status", entry.LastStatus)
	if !ok && entry.LastStatus != ContainerResolved {
		ch, ts, _, _ = w.slackClient.SendMessage(ch, slack.MsgOptionText(writer.String(), false))
		entry.ThreadID = fmt.Sprintf("%s:%s", ch, ts)
		mapReports[entry.Hash] = entry
		// send notification
	}
	if ok {
		channelThread := strings.Split(mapReports[entry.Hash].ThreadID, ":")
		if len(channelThread) >= 2 {
			ch, ts = channelThread[0], channelThread[1]
		}
		if entry.LastStatus == ContainerResolved {
			if ch != "" && ts != "" {
				err := w.slackClient.AddReaction("white_check_mark", slack.ItemRef{Channel: ch, Timestamp: ts})
				_, _, _, _ = w.slackClient.SendMessage(ch, slack.MsgOptionText("this pod has been resolved", false), slack.MsgOptionTS(ts))
				if err != nil {
					logger.Error(err, "unable to add reaction")
					return err
				}
			}
			delete(mapReports, entry.Hash)
		} else if entry.LastStatus != mapReports[entry.Hash].LastStatus {
			ts = mapReports[entry.Hash].ThreadID
			_, _, _, _ = w.slackClient.UpdateMessage(ch, ts, slack.MsgOptionText(writer.String(), false))
			_, _, _, _ = w.slackClient.SendMessage(ch, slack.MsgOptionText(writer.String(), false), slack.MsgOptionTS(ts))
		}
	}
	return nil
}

func indexField(indexer client.FieldIndexer, obj client.Object, field string, extractor func(event *v1.Event) string) error {
	return indexer.IndexField(context.TODO(), obj, field, func(obj client.Object) []string {
		event, ok := obj.(*v1.Event)
		if !ok {
			return nil
		}
		return []string{extractor(event)}
	})
}
func (w *Watcher) SetupIndex(mgr manager.Manager) error {
	idxManager := mgr.GetFieldIndexer()

	indexers := []struct {
		obj       client.Object
		field     string
		extractor func(event *v1.Event) string
	}{
		{&v1.Event{}, "involvedObject.name", func(event *v1.Event) string { return event.InvolvedObject.Name }},
		{&v1.Event{}, "involvedObject.namespace", func(event *v1.Event) string { return event.InvolvedObject.Namespace }},
		{&v1.Event{}, "type", func(event *v1.Event) string { return event.Type }},
	}

	for _, indexer := range indexers {
		if err := indexField(idxManager, indexer.obj, indexer.field, indexer.extractor); err != nil {
			return err
		}
	}
	return nil
}
