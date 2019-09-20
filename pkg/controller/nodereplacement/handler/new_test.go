package handler

import (
	"sync"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	navarchosv1alpha1 "github.com/pusher/navarchos/pkg/apis/navarchos/v1alpha1"
	"github.com/pusher/navarchos/test/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/util/taints"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var _ = Describe("new node replacement handler", func() {
	var m utils.Matcher
	var h *NodeReplacementHandler
	var opts *Options

	var nodeReplacement *navarchosv1alpha1.NodeReplacement
	var highPriorityNR *navarchosv1alpha1.NodeReplacement
	var inProgressNR *navarchosv1alpha1.NodeReplacement
	var mgrStopped *sync.WaitGroup
	var stopMgr chan struct{}

	var workerNode1 *corev1.Node
	var workerNode2 *corev1.Node
	var pod1 *corev1.Pod
	var pod2 *corev1.Pod
	var pod3 *corev1.Pod
	var pod4 *corev1.Pod

	const timeout = time.Second * 5
	const consistentlyTimeout = time.Second

	var newPod = func(name string, node *corev1.Node) *corev1.Pod {
		pod := utils.ExamplePod.DeepCopy()
		pod.Name = name
		pod.Spec.NodeName = node.Name
		return pod
	}

	var setPodRunning = func(obj utils.Object) utils.Object {
		pod, _ := obj.(*corev1.Pod)
		pod.Status.Phase = corev1.PodRunning
		return pod
	}

	var setPodSucceeded = func(obj utils.Object) utils.Object {
		pod, _ := obj.(*corev1.Pod)
		pod.Status.Phase = corev1.PodSucceeded
		return pod
	}

	BeforeEach(func() {
		mgr, err := manager.New(cfg, manager.Options{})
		Expect(err).NotTo(HaveOccurred())
		c, err := client.New(cfg, client.Options{})
		Expect(err).ToNot(HaveOccurred())
		m = utils.Matcher{Client: c}

		stopMgr, mgrStopped = StartTestManager(mgr)

		grace := 5 * time.Second
		opts = &Options{
			EvictionGracePeriod: &grace,
		}

		// Create a node to act as owners for the NodeReplacements created
		workerNode1 = utils.ExampleNodeWorker1.DeepCopy()
		workerNode2 = utils.ExampleNodeWorker2.DeepCopy()
		m.Create(workerNode1).Should(Succeed())
		m.Create(workerNode2).Should(Succeed())

		// Create some pods attached to the nodes
		pod1 = newPod("pod-1", workerNode1)
		pod2 = newPod("pod-2", workerNode1)
		pod3 = newPod("pod-3", workerNode1)
		pod4 = newPod("pod-4", workerNode2)
		m.Create(pod1).Should(Succeed())
		m.Create(pod2).Should(Succeed())
		m.Create(pod3).Should(Succeed())
		m.Create(pod4).Should(Succeed())
		m.UpdateStatus(pod1, setPodRunning, timeout).Should(Succeed())
		m.UpdateStatus(pod2, setPodRunning, timeout).Should(Succeed())
		m.UpdateStatus(pod3, setPodRunning, timeout).Should(Succeed())
		m.UpdateStatus(pod4, setPodRunning, timeout).Should(Succeed())

		nodeReplacement = utils.ExampleNodeReplacement.DeepCopy()
		nodeReplacement.SetOwnerReferences([]metav1.OwnerReference{utils.GetOwnerReferenceForNode(workerNode1)})
		m.Create(nodeReplacement).Should(Succeed())

		highPriorityNR = utils.ExampleNodeReplacement.DeepCopy()
		inProgressNR = utils.ExampleNodeReplacement.DeepCopy()

		highPriorityNR.Spec.ReplacementSpec.Priority = intPtr(10)
		highPriorityNR.SetName("high-priority")

		inProgressNR.Status.Phase = navarchosv1alpha1.ReplacementPhaseInProgress
		inProgressNR.SetName("in-progress")

		m.Create(highPriorityNR).Should(Succeed())
		m.Create(inProgressNR).Should(Succeed())
	})

	AfterEach(func() {
		close(stopMgr)
		mgrStopped.Wait()

		pods := &corev1.PodList{}
		m.List(pods).Should(Succeed())
		for _, pod := range pods.Items {
			m.UpdateStatus(&pod, setPodSucceeded, timeout).Should(Succeed())
		}

		utils.DeleteAll(cfg, timeout,
			&navarchosv1alpha1.NodeReplacementList{},
			&corev1.NodeList{},
			&corev1.PodList{},
			&appsv1.DaemonSetList{},
			&policyv1beta1.PodDisruptionBudgetList{},
		)

		m.Eventually(&corev1.PodList{}, timeout).Should(utils.WithListItems(BeEmpty()))
	})

	JustBeforeEach(func() {
		h = NewNodeReplacementHandler(m.Client, opts)
	})

	Context("shouldProcess", func() {
		var proceed bool
		var reason string
		var replacements *navarchosv1alpha1.NodeReplacementList

		JustBeforeEach(func() {
			proceed, reason = shouldProcess(nodeReplacement, replacements)
		})
		Context("if a another NodeReplacement is higher priority", func() {
			BeforeEach(func() {
				m.Update(nodeReplacement, func(obj utils.Object) utils.Object {
					nr, _ := obj.(*navarchosv1alpha1.NodeReplacement)
					nr.Spec.ReplacementSpec.Priority = intPtr(0)
					return nr
				}, timeout).Should(Succeed())

				replacements = &navarchosv1alpha1.NodeReplacementList{
					Items: []navarchosv1alpha1.NodeReplacement{
						*nodeReplacement, *highPriorityNR,
					},
				}
			})

			It("sets proceed to false", func() {
				Expect(proceed).To(BeFalse())
			})

			It("requeues the NodeReplacement in the result", func() {
				Expect(reason).To(Equal("NodeReplacement \"high-priority\" has a higher priority"))
			})
		})

		Context("if a another NodeReplacement is in Phase InProgress", func() {
			BeforeEach(func() {
				replacements = &navarchosv1alpha1.NodeReplacementList{
					Items: []navarchosv1alpha1.NodeReplacement{
						*nodeReplacement, *inProgressNR,
					},
				}
			})

			It("sets proceed to false", func() {
				Expect(proceed).To(BeFalse())
			})

			It("requeues the NodeReplacement", func() {
				Expect(reason).To(Equal("NodeReplacement \"in-progress\" is already in-progress"))
			})
		})

		Context("if the NodeReplacement should proceed", func() {
			BeforeEach(func() {
				replacements = &navarchosv1alpha1.NodeReplacementList{
					Items: []navarchosv1alpha1.NodeReplacement{
						*nodeReplacement,
					},
				}
			})

			It("sets proceed to true", func() {
				Expect(proceed).To(BeTrue())
			})

			It("does not set the reason string", func() {
				Expect(reason).To(Equal(""))
			})
		})
	})

	Context("getNode", func() {
		var node *corev1.Node
		var exists bool
		var existsErr error

		JustBeforeEach(func() {
			node, exists, existsErr = h.getNode(nodeReplacement)
		})

		Context("when there is an error thrown", func() {
			Context("and the reason for the error is 'NotFound'", func() {
				BeforeEach(func() {
					m.Update(nodeReplacement, func(obj utils.Object) utils.Object {
						nr, _ := obj.(*navarchosv1alpha1.NodeReplacement)
						nr.Spec.NodeName = "does-not-exist"
						return nr
					}, timeout).Should(Succeed())
				})

				It("sets exists to false", func() {
					Expect(exists).To(BeFalse())
				})

				It("does not set an error", func() {
					Expect(existsErr).ToNot(HaveOccurred())
				})

				It("does not return a node", func() {
					Expect(node).To(BeNil())
				})
			})

			PContext("there is another reason for the error", func() {
				BeforeEach(func() {
					// I need to find a way to do this?
				})

				It("sets exists to false", func() {
					Expect(exists).To(BeFalse())
				})

				It("sets an error", func() {
					Expect(existsErr).To(HaveOccurred())
				})

				It("does not return a node", func() {
					Expect(node).To(BeNil())
				})
			})
		})

		Context("when the node exists", func() {
			BeforeEach(func() {
				m.Update(nodeReplacement, func(obj utils.Object) utils.Object {
					nr, _ := obj.(*navarchosv1alpha1.NodeReplacement)
					nr.Spec.NodeUID = workerNode1.GetUID()
					nr.Spec.NodeName = workerNode1.GetName()
					return nr
				}, timeout).Should(Succeed())
			})

			It("sets exists to true", func() {
				Expect(exists).To(BeTrue())
			})

			It("does not set an error", func() {
				Expect(existsErr).ToNot(HaveOccurred())
			})

			It("returns the node", func() {
				Expect(node.GetName()).To(Equal(workerNode1.GetName()))
				Expect(node.GetUID()).To(Equal(workerNode1.GetUID()))
			})
		})
	})

	Context("cordonNode", func() {
		var err error

		Context("when called on an uncordoned node", func() {
			JustBeforeEach(func() {
				err = h.cordonNode(workerNode2)
			})
			It("should cordon the node", func() {
				m.Eventually(workerNode2, timeout).Should(utils.WithField("Spec.Unschedulable", BeTrue()))
				m.Eventually(workerNode2, timeout).Should(utils.WithField("Spec.Taints",
					ContainElement(SatisfyAll(
						utils.WithField("Effect", Equal(corev1.TaintEffect("NoSchedule"))),
						utils.WithField("Key", Equal("node.kubernetes.io/unschedulable")),
					)),
				))
			})

			It("should not return an error", func() {
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when called on a cordoned node", func() {
			var cordonedNode *corev1.Node
			BeforeEach(func() {
				cordonedNode = utils.ExampleNodeMaster1.DeepCopy()
				m.Create(cordonedNode).Should(Succeed())
				m.Update(cordonedNode, func(obj utils.Object) utils.Object {
					node, _ := obj.(*corev1.Node)
					node.Spec.Unschedulable = true
					node, _, _ = taints.AddOrUpdateTaint(node, &corev1.Taint{
						Key:    "node.kubernetes.io/unschedulable",
						Effect: corev1.TaintEffect("NoSchedule"),
					})
					return node
				}, timeout).Should(Succeed())
			})

			It("should cordon the node", func() {
				m.Consistently(cordonedNode, timeout).Should(utils.WithField("Spec.Unschedulable", BeTrue()))
				m.Consistently(cordonedNode, timeout).Should(utils.WithField("Spec.Taints",
					ContainElement(SatisfyAll(
						utils.WithField("Effect", Equal(corev1.TaintEffect("NoSchedule"))),
						utils.WithField("Key", Equal("node.kubernetes.io/unschedulable")),
					)),
				))
			})

			It("should not return an error", func() {
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Context("processPods", func() {
		var nodePods []string
		var ignoredPods []navarchosv1alpha1.PodReason
		var daemonset *appsv1.DaemonSet

		var err error
		BeforeEach(func() {
			daemonset = utils.ExampleDaemonSet.DeepCopy()
			m.Create(daemonset).Should(Succeed())
			m.Update(pod2, func(obj utils.Object) utils.Object {
				pod, _ := obj.(*corev1.Pod)
				pod.SetOwnerReferences([]metav1.OwnerReference{utils.GetOwnerReferenceForDaemonSet(daemonset)})
				return pod
			}, timeout).Should(Succeed())
		})

		JustBeforeEach(func() {
			nodePods, ignoredPods, err = h.processPods(workerNode1)
		})

		It("sets NodePods", func() {
			Expect(nodePods).To(ConsistOf(
				"pod-1",
				"pod-2",
				"pod-3",
			))
		})

		It("sets IgnoredPods", func() {
			Expect(ignoredPods).To(ConsistOf(
				navarchosv1alpha1.PodReason{Name: "pod-2", Reason: "pod owned by a DaemonSet"}))
		})

		It("should not return an error", func() {
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
