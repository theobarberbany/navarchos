package status

import navarchosv1alpha1 "github.com/pusher/navarchos/pkg/apis/navarchos/v1alpha1"

// Result is used as the basis to updating the status of the NodeRollout.
// It contains information gathered during a single run of the reconcile loop.
type Result struct {
	// This represents the Phase of the NodeRollout that the status should be set
	// to when updating the status.
	// If Phase == nil, don't update the Phase, else, overwrite it.
	Phase *navarchosv1alpha1.NodeRolloutPhase

	// This should contain any errors related to the creation of the NodeReplacements.
	ReplacementsCompletedError error

	// This is the short reason description for the errors related to creationg of
	// NodeReplacements.
	ReplacementsCompletedReason string

	// This should list all NodeReplacements created.
	// This will be a list of the node names that are going to be replaced.
	// This should only be set on the first pass of the controller while the
	// NodeRollout is in Phase New.
	ReplacementsCreated []string

	// This should be a list of any newly completed NodeReplacements.
	// This will be any node name that is in the ReplacementsCreated list but
	// does not exist on the cluster.
	// This list will be merged with the existing status list.
	ReplacementsCompleted []string

	// This should be a list of any currently failing NodeReplacements.
	// This list will replace the existing status list.
	ReplacementsFailed []string
}