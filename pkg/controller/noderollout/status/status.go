package status

import (
	"context"
	"fmt"
	"reflect"

	navarchosv1alpha1 "github.com/pusher/navarchos/pkg/apis/navarchos/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateStatus merges the status in the existing instance with the information
// provided in the Result and then updates the instance if there is any
// difference between the new and updated status
func UpdateStatus(c client.Client, instance *navarchosv1alpha1.NodeRollout, result *Result) error {
	status := instance.Status

	setPhase(&status, result)

	err := setReplacementsCreated(&status, result)
	if err != nil {
		return err
	}

	setReplacementsCompleted(&status, result)

	err = setCompletionTimestamp(&status, result)
	if err != nil {
		return err
	}

	err = setCreatedCondition(&status, result)
	if err != nil {
		return err
	}
	err = setInProgressCondition(&status, result)
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(status, instance.Status) {
		copy := instance.DeepCopy()
		copy.Status = status

		err := c.Update(context.TODO(), copy)
		if err != nil {
			return fmt.Errorf("error updating status: %v", err)
		}
	}

	return nil
}

// setPhase sets the phase when it is set in the Result
func setPhase(status *navarchosv1alpha1.NodeRolloutStatus, result *Result) {
	if result.Phase != nil {
		status.Phase = *result.Phase
	}
}

// setReplacementsCreated sets the ReplacementsCreated, provided it has not been
// set before
func setReplacementsCreated(status *navarchosv1alpha1.NodeRolloutStatus, result *Result) error {
	if status.ReplacementsCreated != nil && result.ReplacementsCreated != nil {
		return fmt.Errorf("cannot update ReplacementsCreated, field is immutable once set")
	}

	if status.ReplacementsCreated == nil && result.ReplacementsCreated != nil {
		status.ReplacementsCreated = result.ReplacementsCreated
		status.ReplacementsCreatedCount = len(result.ReplacementsCreated)
	}

	return nil
}

// setReplacementsCompleted sets the ReplacementsCompleted, if it has not been
// set before it is added. If it has been set before the two are appended
func setReplacementsCompleted(status *navarchosv1alpha1.NodeRolloutStatus, result *Result) {
	if status.ReplacementsCompleted != nil && result.ReplacementsCompleted != nil {
		status.ReplacementsCompleted = appendIfMissingStr(status.ReplacementsCompleted, result.ReplacementsCompleted...)
		status.ReplacementsCompletedCount = len(status.ReplacementsCompleted)
	}

	if status.ReplacementsCompleted == nil && result.ReplacementsCompleted != nil {
		status.ReplacementsCompleted = result.ReplacementsCompleted
		status.ReplacementsCompletedCount = len(result.ReplacementsCompleted)
	}

}

// setCompletionTimestamp sets the setCompletionTimestamp field. If it has not
// been set before it is added. If it has been set before an error is returned
func setCompletionTimestamp(status *navarchosv1alpha1.NodeRolloutStatus, result *Result) error {
	if status.CompletionTimestamp != nil && result.CompletionTimestamp != nil {
		return fmt.Errorf("cannot update CompletionTimestamp, field is immutable once set")
	}

	if status.CompletionTimestamp == nil && result.CompletionTimestamp != nil {
		status.CompletionTimestamp = result.CompletionTimestamp
	}
	return nil
}

// newNodeRolloutCondition creates a new condition NodeRolloutCondition
func newNodeRolloutCondition(condType navarchosv1alpha1.NodeRolloutConditionType, status corev1.ConditionStatus, reason navarchosv1alpha1.NodeRolloutConditionReason, message string) navarchosv1alpha1.NodeRolloutCondition {
	return navarchosv1alpha1.NodeRolloutCondition{
		Type:               condType,
		Status:             status,
		LastUpdateTime:     metav1.Now(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
}

// getNodeRolloutCondition returns the condition with the provided type
func getNodeRolloutCondition(status navarchosv1alpha1.NodeRolloutStatus, condType navarchosv1alpha1.NodeRolloutConditionType) *navarchosv1alpha1.NodeRolloutCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// setNodeRolloutCondition updates the NodeRollout to include the provided condition. If the condition that
// we are about to add already exists and has the same status and reason then we are not going to update
func setNodeRolloutCondition(status *navarchosv1alpha1.NodeRolloutStatus, condition navarchosv1alpha1.NodeRolloutCondition) {
	currentCond := getNodeRolloutCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason {
		return
	}
	// Do not update lastTransitionTime if the status of the condition doesn't change
	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
}

// filterOutCondition returns a new slice of NodeRollout conditions without conditions with the provided types
func filterOutCondition(conditions []navarchosv1alpha1.NodeRolloutCondition, condType navarchosv1alpha1.NodeRolloutConditionType) []navarchosv1alpha1.NodeRolloutCondition {
	var newConditions []navarchosv1alpha1.NodeRolloutCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}

func setInProgressCondition(status *navarchosv1alpha1.NodeRolloutStatus, result *Result) error {
	if result.ReplacementsInProgressError != nil && result.ReplacementsInProgressReason == "" {
		return fmt.Errorf("if ReplacementsInProgressError is set, ReplacementsInProgressReason must also be set")
	}

	if result.ReplacementsInProgressReason != "" {
		condition := newNodeRolloutCondition(navarchosv1alpha1.ReplacementsInProgressType, corev1.ConditionTrue, result.ReplacementsInProgressReason, "")

		if result.ReplacementsInProgressError != nil {
			condition.Status = corev1.ConditionFalse
			condition.Message = result.ReplacementsInProgressError.Error()
		}

		if result.ReplacementsInProgressReason == "ReplacementsCompleted" {
			condition.Status = corev1.ConditionFalse
		}

		setNodeRolloutCondition(status, condition)
	}

	return nil
}

func setCreatedCondition(status *navarchosv1alpha1.NodeRolloutStatus, result *Result) error {
	if result.ReplacementsCreatedError != nil && result.ReplacementsCreatedReason == "" {
		return fmt.Errorf("if ReplacementsCreatedError is set, ReplacementsCreatedReason must also be set")
	}

	if result.ReplacementsCreatedReason != "" {
		condition := newNodeRolloutCondition(navarchosv1alpha1.ReplacementsCreatedType, corev1.ConditionTrue, result.ReplacementsCreatedReason, "")

		if result.ReplacementsCreatedError != nil {
			condition.Status = corev1.ConditionFalse
			condition.Message = result.ReplacementsCreatedError.Error()
		}

		setNodeRolloutCondition(status, condition)
	}

	return nil
}

// appendIfMissingStr will append two []string(s) dropping duplicate elements
func appendIfMissingStr(slice []string, str ...string) []string {
	merged := slice
	for _, ele := range str {
		merged = appendIfMissingElement(merged, ele)
	}
	return merged
}

// appendIfMissingElement will append a string to a []string only if it is
// unique
func appendIfMissingElement(slice []string, i string) []string {
	for _, ele := range slice {
		if ele == i {
			return slice
		}
	}
	return append(slice, i)
}
