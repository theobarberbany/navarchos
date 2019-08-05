/*
Copyright 2019 Pusher Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package handler

import (
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	navarchosv1alpha1 "github.com/pusher/navarchos/pkg/apis/navarchos/v1alpha1"
	"github.com/pusher/navarchos/pkg/controller/noderollout/status"
	"github.com/pusher/navarchos/test/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var _ = Describe("Handler suite", func() {
	var m utils.Matcher
	var h *NodeRolloutHandler
	var opts *Options
	var result *status.Result

	var nodeRollout *navarchosv1alpha1.NodeRollout
	var mgrStopped *sync.WaitGroup
	var stopMgr chan struct{}

	var masterNode1 *corev1.Node
	var masterNode2 *corev1.Node
	var workerNode1 *corev1.Node
	var workerNode2 *corev1.Node
	var otherNode *corev1.Node

	const timeout = time.Second * 5
	const consistentlyTimeout = time.Second

	// checkForNodeReplacement checks if a NodeReplacement exists with the given
	// name, an owner reference pointing to the given node, and the given priority
	var checkForNodeReplacement = func(name string, owner *corev1.Node, priority int) {
		nrList := &navarchosv1alpha1.NodeReplacementList{}
		m.List(nrList, &client.ListOptions{}, timeout).Should(Succeed())

		Expect(nrList.Items).To(ContainElement(SatisfyAll(
			utils.WithNodeReplacementSpecField("Priority", Equal(priority)),
			utils.WithNodeReplacementSpecField("NodeName", Equal(owner.GetName())),
			utils.WithNodeReplacementSpecField("NodeUID", Equal(owner.GetUID())),
			utils.WithObjectMetaField("OwnerReferences", SatisfyAll(
				ContainElement(Equal(utils.GetOwnerReferenceForNode(owner))),
				ContainElement(Equal(utils.GetOwnerReferenceForNodeRollout(nodeRollout))),
			)),
		)))
	}

	var nodeReplacementFor = func(node *corev1.Node) *navarchosv1alpha1.NodeReplacement {
		return &navarchosv1alpha1.NodeReplacement{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: fmt.Sprintf("%s-", node.Name),
				OwnerReferences: []metav1.OwnerReference{
					utils.GetOwnerReferenceForNode(node),
					utils.GetOwnerReferenceForNodeRollout(nodeRollout),
				},
			},
		}
	}

	BeforeEach(func() {
		mgr, err := manager.New(cfg, manager.Options{})
		Expect(err).NotTo(HaveOccurred())
		c := mgr.GetClient()
		m = utils.Matcher{Client: c}

		stopMgr, mgrStopped = StartTestManager(mgr)

		opts = &Options{}

		nodeRollout = utils.ExampleNodeRollout.DeepCopy()
		m.Create(nodeRollout).Should(Succeed())

		// Create some nodes to act as owners for the NodeReplacements created
		masterNode1 = utils.ExampleNodeMaster1.DeepCopy()
		masterNode2 = utils.ExampleNodeMaster2.DeepCopy()
		workerNode1 = utils.ExampleNodeWorker1.DeepCopy()
		workerNode2 = utils.ExampleNodeWorker2.DeepCopy()
		otherNode = utils.ExampleNodeOther.DeepCopy()

		m.Create(masterNode1).Should(Succeed())
		m.Create(masterNode2).Should(Succeed())
		m.Create(workerNode1).Should(Succeed())
		m.Create(workerNode2).Should(Succeed())
		m.Create(otherNode).Should(Succeed())
	})

	AfterEach(func() {
		close(stopMgr)
		mgrStopped.Wait()

		utils.DeleteAll(cfg, timeout,
			&navarchosv1alpha1.NodeRolloutList{},
			&navarchosv1alpha1.NodeReplacementList{},
			&corev1.NodeList{},
		)
	})

	JustBeforeEach(func() {
		h = NewNodeRolloutHandler(m.Client, opts)
	})

	Context("when the Handler function is called on a New NodeRollout", func() {
		JustBeforeEach(func() {
			result = h.Handle(nodeRollout)
		})

		Context("with NodeSelectors only", func() {
			BeforeEach(func() {
				m.Update(nodeRollout, func(obj utils.Object) utils.Object {
					nr, _ := obj.(*navarchosv1alpha1.NodeRollout)
					// This is set by default, so unset before we handle the NodeRollout
					nr.Spec.NodeNames = []navarchosv1alpha1.PriorityName{}
					return nr
				}, timeout).Should(Succeed())
				Expect(nodeRollout).To(utils.WithNodeRolloutSpecField("NodeNames", BeEmpty()))
			})

			PIt("creates a NodeReplacement for example-master-1", func() {
				checkForNodeReplacement("example-master-1", masterNode1, 15)
			})

			PIt("creates a NodeReplacement for example-master-2", func() {
				checkForNodeReplacement("example-master-2", masterNode2, 15)
			})

			PIt("creates a NodeReplacement for example-worker-1", func() {
				checkForNodeReplacement("example-worker-1", workerNode1, 5)
			})

			PIt("creates a NodeReplacement for example-worker-2", func() {
				checkForNodeReplacement("example-worker-2", workerNode2, 5)
			})

			It("does not create a NodeReplacement for example-other", func() {
				nr := &navarchosv1alpha1.NodeReplacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "example-other",
					},
				}
				m.Get(nr, consistentlyTimeout).ShouldNot(Succeed())
			})

			PIt("populates the Result ReplacementsCreated field", func() {
				Expect(result.ReplacementsCreated).To(ConsistOf(
					"example-master-1",
					"example-master-2",
					"example-worker-1",
					"example-worker-2",
				))
			})

			PIt("sets the Result Phase to InProgress", func() {
				Expect(result.Phase).To(Equal(navarchosv1alpha1.RolloutPhaseInProgress))
			})

			It("does not set the Result ReplacementsCompleted field", func() {
				Expect(result.ReplacementsCompleted).To(BeEmpty())
			})

			It("does not set the Result ReplacementsFailed field", func() {
				Expect(result.ReplacementsFailed).To(BeEmpty())
			})

			It("does not set any error", func() {
				Expect(result.ReplacementsCompletedError).To(BeNil())
				Expect(result.ReplacementsCompletedReason).To(BeEmpty())
			})
		})

		Context("with NodeNames only", func() {
			BeforeEach(func() {
				m.Update(nodeRollout, func(obj utils.Object) utils.Object {
					nr, _ := obj.(*navarchosv1alpha1.NodeRollout)
					// This is set by default, so unset before we handle the NodeRollout
					nr.Spec.NodeSelectors = []navarchosv1alpha1.PriorityLabelSelector{}
					return nr
				}, timeout).Should(Succeed())
				Expect(nodeRollout).To(utils.WithNodeRolloutSpecField("NodeSelectors", BeEmpty()))
			})

			PIt("creates a NodeReplacement for example-master-1", func() {
				checkForNodeReplacement("example-master-1", masterNode1, 20)
			})

			It("does not create a NodeReplacement for example-master-2", func() {
				nr := &navarchosv1alpha1.NodeReplacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "example-master-2",
					},
				}
				m.Get(nr, consistentlyTimeout).ShouldNot(Succeed())
			})

			PIt("creates a NodeReplacement for example-worker-1", func() {
				checkForNodeReplacement("example-worker-1", workerNode1, 10)
			})

			It("does not create a NodeReplacement for example-worker-2", func() {
				nr := &navarchosv1alpha1.NodeReplacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "example-worker-2",
					},
				}
				m.Get(nr, consistentlyTimeout).ShouldNot(Succeed())
			})

			It("does not create a NodeReplacement for example-other", func() {
				nr := &navarchosv1alpha1.NodeReplacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "example-other",
					},
				}
				m.Get(nr, consistentlyTimeout).ShouldNot(Succeed())
			})

			PIt("populates the Result ReplacementsCreated field", func() {
				Expect(result.ReplacementsCreated).To(ConsistOf(
					"example-master-1",
					"example-worker-1",
				))
			})

			PIt("sets the Result Phase to InProgress", func() {
				Expect(result.Phase).To(Equal(navarchosv1alpha1.RolloutPhaseInProgress))
			})

			It("does not set the Result ReplacementsCompleted field", func() {
				Expect(result.ReplacementsCompleted).To(BeEmpty())
			})

			It("does not set the Result ReplacementsFailed field", func() {
				Expect(result.ReplacementsFailed).To(BeEmpty())
			})

			It("does not set any error", func() {
				Expect(result.ReplacementsCompletedError).To(BeNil())
				Expect(result.ReplacementsCompletedReason).To(BeEmpty())
			})
		})

		Context("with NodeNames and NodeSelectors", func() {
			BeforeEach(func() {
				Expect(nodeRollout).To(utils.WithNodeRolloutSpecField("NodeNames", Not(BeEmpty())))
				Expect(nodeRollout).To(utils.WithNodeRolloutSpecField("NodeSelectors", Not(BeEmpty())))
			})

			PIt("creates a NodeReplacement for example-master-1", func() {
				checkForNodeReplacement("example-master-1", masterNode1, 20)
			})

			PIt("creates a NodeReplacement for example-master-2", func() {
				checkForNodeReplacement("example-master-2", masterNode2, 15)
			})

			PIt("creates a NodeReplacement for example-worker-1", func() {
				checkForNodeReplacement("example-worker-1", workerNode1, 10)
			})

			PIt("creates a NodeReplacement for example-worker-2", func() {
				checkForNodeReplacement("example-worker-2", workerNode2, 5)
			})

			It("does not create a NodeReplacement for example-other", func() {
				nr := &navarchosv1alpha1.NodeReplacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "example-other",
					},
				}
				m.Get(nr, consistentlyTimeout).ShouldNot(Succeed())
			})

			PIt("populates the Result ReplacementsCreated field", func() {
				Expect(result.ReplacementsCreated).To(ConsistOf(
					"example-master-1",
					"example-master-2",
					"example-worker-1",
					"example-worker-2",
				))
			})

			PIt("sets the Result Phase to InProgress", func() {
				Expect(result.Phase).To(Equal(navarchosv1alpha1.RolloutPhaseInProgress))
			})

			It("does not set the Result ReplacementsCompleted field", func() {
				Expect(result.ReplacementsCompleted).To(BeEmpty())
			})

			It("does not set the Result ReplacementsFailed field", func() {
				Expect(result.ReplacementsFailed).To(BeEmpty())
			})

			It("does not set any error", func() {
				Expect(result.ReplacementsCompletedError).To(BeNil())
				Expect(result.ReplacementsCompletedReason).To(BeEmpty())
			})
		})

		Context("and NodeReplacements already exist for the nodes", func() {
			var nrMaster1, nrWorker1 *navarchosv1alpha1.NodeReplacement

			BeforeEach(func() {
				By("setting the NodeReplacment to NodeNames only")
				m.Update(nodeRollout, func(obj utils.Object) utils.Object {
					nr, _ := obj.(*navarchosv1alpha1.NodeRollout)
					// This is set by default, so unset before we handle the NodeRollout
					nr.Spec.NodeSelectors = []navarchosv1alpha1.PriorityLabelSelector{}
					return nr
				}, timeout).Should(Succeed())
				Expect(nodeRollout).To(utils.WithNodeRolloutSpecField("NodeSelectors", BeEmpty()))

				By("creating NodeReplacements for the Nodes named in the NodeRollout")
				nrMaster1 = nodeReplacementFor(masterNode1)
				m.Create(nrMaster1).Should(Succeed())
				nrWorker1 = nodeReplacementFor(workerNode1)
				m.Create(nrWorker1).Should(Succeed())
			})

			PIt("should create new NodeReplacements for the nodes", func() {
				nrList := &navarchosv1alpha1.NodeReplacementList{}
				m.List(nrList, &client.ListOptions{}, timeout).Should(Succeed())

				items := []*navarchosv1alpha1.NodeReplacement{}
				for _, nr := range nrList.Items {
					items = append(items, nr.DeepCopy())
				}

				Expect(items).To(SatisfyAll(
					ContainElement(SatisfyAll(
						utils.WithObjectMetaField("Name", Not(Equal(nrMaster1.GetName()))),
						utils.WithNodeReplacementSpecField("Priority", Equal(20)),
						utils.WithNodeReplacementSpecField("NodeName", Equal(masterNode1.GetName())),
						utils.WithNodeReplacementSpecField("NodeUID", Equal(masterNode1.GetUID())),
						utils.WithObjectMetaField("OwnerReferences", SatisfyAll(
							ContainElement(Equal(utils.GetOwnerReferenceForNode(masterNode1))),
							ContainElement(Equal(utils.GetOwnerReferenceForNodeRollout(nodeRollout))),
						)),
					)),
					ContainElement(SatisfyAll(
						utils.WithObjectMetaField("Name", Not(Equal(nrWorker1.GetName()))),
						utils.WithNodeReplacementSpecField("Priority", Equal(10)),
						utils.WithNodeReplacementSpecField("NodeName", Equal(workerNode1.GetName())),
						utils.WithNodeReplacementSpecField("NodeUID", Equal(workerNode1.GetUID())),
						utils.WithObjectMetaField("OwnerReferences", SatisfyAll(
							ContainElement(Equal(utils.GetOwnerReferenceForNode(workerNode1))),
							ContainElement(Equal(utils.GetOwnerReferenceForNodeRollout(nodeRollout))),
						)),
					)),
				))
			})
		})
	})

	Context("when the Handler function is called on an InProgress NodeRollout", func() {
		var nrMaster1, nrMaster2, nrWorker1, nrWorker2 *navarchosv1alpha1.NodeReplacement
		BeforeEach(func() {
			nrMaster1 = nodeReplacementFor(masterNode1)
			nrMaster2 = nodeReplacementFor(masterNode2)
			nrWorker1 = nodeReplacementFor(workerNode1)
			nrWorker2 = nodeReplacementFor(workerNode2)
			m.Create(nrMaster1).Should(Succeed())
			m.Create(nrMaster2).Should(Succeed())
			m.Create(nrWorker1).Should(Succeed())
			m.Create(nrWorker2).Should(Succeed())

			// Set the NodeRollout as we expect it to be at this point
			m.Update(nodeRollout, func(obj utils.Object) utils.Object {
				nr, _ := obj.(*navarchosv1alpha1.NodeRollout)
				nr.Status.Phase = navarchosv1alpha1.RolloutPhaseInProgress
				nr.Status.ReplacementsCreated = []string{"example-master-1", "example-master-2", "example-worker-1", "example-worker-2"}
				nr.Status.ReplacementsCreatedCount = len(nr.Status.ReplacementsCreated)
				return nr
			}, timeout).Should(Succeed())
			Expect(nodeRollout.Status.Phase).To(Equal(navarchosv1alpha1.RolloutPhaseInProgress))
		})

		JustBeforeEach(func() {
			result = h.Handle(nodeRollout)
		})

		Context("if nothing has changed", func() {
			It("does not set the Result ReplacementsCompleted field", func() {
				Expect(result.ReplacementsCompleted).To(BeEmpty())
			})

			It("does not set the Result ReplacementsFailed field", func() {
				Expect(result.ReplacementsFailed).To(BeEmpty())
			})
		})

		Context("if a NodeReplacement has been marked as Completed", func() {
			BeforeEach(func() {
				m.Update(nrMaster1, func(obj utils.Object) utils.Object {
					nr, _ := obj.(*navarchosv1alpha1.NodeReplacement)
					nr.Status.Phase = navarchosv1alpha1.ReplacementPhaseCompleted
					return nr
				}, timeout).Should(Succeed())
			})

			PIt("list the completed NodeReplacement in the Result ReplacementsCompleted field", func() {
				Expect(result.ReplacementsCompleted).To(ConsistOf("example-master-1"))
			})

			It("does not set the Result ReplacementsFailed field", func() {
				Expect(result.ReplacementsFailed).To(BeEmpty())
			})
		})

		Context("if a NodeReplacement has been marked as failed", func() {
			BeforeEach(func() {
				m.Update(nrMaster1, func(obj utils.Object) utils.Object {
					nr, _ := obj.(*navarchosv1alpha1.NodeReplacement)
					nr.Status.Phase = navarchosv1alpha1.ReplacementPhaseFailed
					return nr
				}, timeout).Should(Succeed())
			})

			It("does not set the Result ReplacementsCompleted field", func() {
				Expect(result.ReplacementsCompleted).To(BeEmpty())
			})

			PIt("list the Failed NodeReplacement in the Result ReplacementsFailed field", func() {
				Expect(result.ReplacementsFailed).To(ConsistOf("example-master-1"))
			})
		})

		Context("once all NodeReplacements are marked as Completed or Failed", func() {
			BeforeEach(func() {
				for _, nr := range []*navarchosv1alpha1.NodeReplacement{nrMaster1, nrMaster2, nrWorker1, nrWorker2} {
					m.Update(nr, func(obj utils.Object) utils.Object {
						nr, _ := obj.(*navarchosv1alpha1.NodeReplacement)
						nr.Status.Phase = navarchosv1alpha1.ReplacementPhaseCompleted
						return nr
					}, timeout).Should(Succeed())
				}
			})

			Context("if all NodeReplacements have been marked as Completed", func() {
				PIt("lists the completed NodeReplacements in the Result ReplacementsCompleted field", func() {
					Expect(result.ReplacementsCompleted).To(ConsistOf(
						"example-master-1",
						"example-master-2",
						"example-worker-1",
						"example-worker-2",
					))
				})

				It("does not set the Result ReplacementsFailed field", func() {
					Expect(result.ReplacementsFailed).To(BeEmpty())
				})

				PIt("sets the Result Phase field to Completed", func() {
					Expect(result.Phase).To(Equal(navarchosv1alpha1.RolloutPhaseCompleted))
				})
			})

			Context("if at least one NodeReplacements have been marked as Failed", func() {
				BeforeEach(func() {
					m.Update(nodeReplacementFor(masterNode1), func(obj utils.Object) utils.Object {
						nr, _ := obj.(*navarchosv1alpha1.NodeReplacement)
						nr.Status.Phase = navarchosv1alpha1.ReplacementPhaseFailed
						return nr
					}, timeout).Should(Succeed())
				})

				PIt("lists the Completed NodeReplacements in the Result ReplacementsCompleted field", func() {
					Expect(result.ReplacementsCompleted).To(ConsistOf(
						"example-master-2",
						"example-worker-1",
						"example-worker-2",
					))
				})

				PIt("lists the Failed NodeReplacements in the Result ReplacementsFailed field", func() {
					Expect(result.ReplacementsFailed).To(ConsistOf("example-master-1"))
				})

				PIt("sets the Result Phase field to Failed", func() {
					Expect(result.Phase).To(Equal(navarchosv1alpha1.RolloutPhaseFailed))
				})
			})
		})
	})

	Context("when the Handler function is called on a Completed NodeRollout", func() {
		BeforeEach(func() {
			m.Create(nodeReplacementFor(masterNode1)).Should(Succeed())
			m.Create(nodeReplacementFor(masterNode2)).Should(Succeed())
			m.Create(nodeReplacementFor(workerNode1)).Should(Succeed())
			m.Create(nodeReplacementFor(workerNode2)).Should(Succeed())

			// Set the NodeRollout as we expect it to be at this point
			m.Update(nodeRollout, func(obj utils.Object) utils.Object {
				nr, _ := obj.(*navarchosv1alpha1.NodeRollout)
				nr.Status.Phase = navarchosv1alpha1.RolloutPhaseCompleted
				nr.Status.ReplacementsCreated = []string{"example-master-1", "example-master-2", "example-worker-1", "example-worker-2"}
				nr.Status.ReplacementsCreatedCount = len(nr.Status.ReplacementsCreated)
				nr.Status.ReplacementsCompleted = nr.Status.ReplacementsCreated
				nr.Status.ReplacementsCompletedCount = len(nr.Status.ReplacementsCompleted)
				return nr
			}, timeout).Should(Succeed())
			Expect(nodeRollout.Status.Phase).To(Equal(navarchosv1alpha1.RolloutPhaseCompleted))
		})

		JustBeforeEach(func() {
			result = h.Handle(nodeRollout)
		})

		Context("and the NodeRollout is younger than the maximum age", func() {
			It("does nothing", func() {
				Expect(result).To(Equal(&status.Result{}))
			})
		})

		Context("and the NodeRollout is older than the maximum age", func() {
			BeforeEach(func() {
				nodeRollout.CreationTimestamp = metav1.NewTime(time.Now().Add(-h.maxAge - time.Hour))
			})

			PIt("deletes the NodeRollout", func() {
				m.Get(nodeRollout, timeout).ShouldNot(Succeed())
			})
		})

	})

	Context("when the Handler function is called on a Failed NodeRollout", func() {
		BeforeEach(func() {
			m.Create(nodeReplacementFor(masterNode1)).Should(Succeed())
			m.Create(nodeReplacementFor(masterNode2)).Should(Succeed())
			m.Create(nodeReplacementFor(workerNode1)).Should(Succeed())
			m.Create(nodeReplacementFor(workerNode2)).Should(Succeed())

			// Set the NodeRollout as we expect it to be at this point
			m.Update(nodeRollout, func(obj utils.Object) utils.Object {
				nr, _ := obj.(*navarchosv1alpha1.NodeRollout)
				nr.Status.Phase = navarchosv1alpha1.RolloutPhaseFailed
				nr.Status.ReplacementsCreated = []string{"example-master-1", "example-master-2", "example-worker-1", "example-worker-2"}
				nr.Status.ReplacementsCreatedCount = len(nr.Status.ReplacementsCreated)
				nr.Status.ReplacementsCompleted = []string{"example-master-2", "example-worker-1", "example-worker-2"}
				nr.Status.ReplacementsCompletedCount = len(nr.Status.ReplacementsCompleted)
				nr.Status.ReplacementsFailed = []string{"example-master-1"}
				nr.Status.ReplacementsFailedCount = len(nr.Status.ReplacementsFailed)
				return nr
			}, timeout).Should(Succeed())
			Expect(nodeRollout.Status.Phase).To(Equal(navarchosv1alpha1.RolloutPhaseFailed))
		})

		JustBeforeEach(func() {
			result = h.Handle(nodeRollout)
		})

		Context("and the NodeRollout is younger than the maximum age", func() {
			It("does nothing", func() {
				Expect(result).To(Equal(&status.Result{}))
			})
		})

		Context("and the NodeRollout is older than the maximum age", func() {
			BeforeEach(func() {
				nodeRollout.CreationTimestamp = metav1.NewTime(time.Now().Add(h.maxAge - time.Hour))
			})

			PIt("deletes the NodeRollout", func() {
				m.Get(nodeRollout, timeout).ShouldNot(Succeed())
			})
		})
	})
})