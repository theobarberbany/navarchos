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

package utils

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/onsi/gomega"
	gtypes "github.com/onsi/gomega/types"
	navarchosv1alpha1 "github.com/pusher/navarchos/pkg/apis/navarchos/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Matcher has Gomega Matchers that use the controller-runtime client
type Matcher struct {
	Client client.Client
}

// Object is the combination of two interfaces as a helper for passing
// Kubernetes objects between methods
type Object interface {
	runtime.Object
	metav1.Object
}

// UpdateFunc modifies the object fetched from the API server before sending
// the update
type UpdateFunc func(Object) Object

// Create creates the object on the API server
func (m *Matcher) Create(obj Object, extras ...interface{}) gomega.GomegaAssertion {
	err := m.Client.Create(context.TODO(), obj)
	return gomega.Expect(err, extras)
}

// Delete deletes the object from the API server
func (m *Matcher) Delete(obj Object, extras ...interface{}) gomega.GomegaAssertion {
	err := m.Client.Delete(context.TODO(), obj)
	return gomega.Expect(err, extras)
}

// Update udpates the object on the API server by fetching the object
// and applying a mutating UpdateFunc before sending the update
func (m *Matcher) Update(obj Object, fn UpdateFunc, intervals ...interface{}) gomega.GomegaAsyncAssertion {
	key := types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}
	update := func() error {
		err := m.Client.Get(context.TODO(), key, obj)
		if err != nil {
			return err
		}
		return m.Client.Update(context.TODO(), fn(obj))
	}
	return gomega.Eventually(update, intervals...)
}

// UpdateStatus udpates the object's status subresource on the API server by
// fetching the object and applying a mutating UpdateFunc before sending the
// update
func (m *Matcher) UpdateStatus(obj Object, fn UpdateFunc, intervals ...interface{}) gomega.GomegaAsyncAssertion {
	key := types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}
	update := func() error {
		err := m.Client.Get(context.TODO(), key, obj)
		if err != nil {
			return err
		}
		return m.Client.Status().Update(context.TODO(), fn(obj))
	}
	return gomega.Eventually(update, intervals...)
}

// Get gets the object from the API server
func (m *Matcher) Get(obj Object, intervals ...interface{}) gomega.GomegaAsyncAssertion {
	key := types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}
	get := func() error {
		return m.Client.Get(context.TODO(), key, obj)
	}
	return gomega.Eventually(get, intervals...)
}

// List gets the list object from the API server
func (m *Matcher) List(obj runtime.Object, intervals ...interface{}) gomega.GomegaAsyncAssertion {
	list := func() error {
		return m.Client.List(context.TODO(), obj)
	}
	return gomega.Eventually(list, intervals...)
}

// Consistently continually gets the object from the API for comparison
func (m *Matcher) Consistently(obj Object, intervals ...interface{}) gomega.GomegaAsyncAssertion {
	return m.consistentlyObject(obj, intervals...)
}

// consistentlyObject gets an individual object from the API server
func (m *Matcher) consistentlyObject(obj Object, intervals ...interface{}) gomega.GomegaAsyncAssertion {
	key := types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}
	get := func() Object {
		err := m.Client.Get(context.TODO(), key, obj)
		if err != nil {
			panic(err)
		}
		return obj
	}
	return gomega.Consistently(get, intervals...)
}

// Eventually continually gets the object from the API for comparison
func (m *Matcher) Eventually(obj runtime.Object, intervals ...interface{}) gomega.GomegaAsyncAssertion {
	// If the object is a list, return a list
	if meta.IsListType(obj) {
		return m.eventuallyList(obj, intervals...)
	}
	if o, ok := obj.(Object); ok {
		return m.eventuallyObject(o, intervals...)
	}
	//Should not get here
	panic("Unknown object.")
}

// eventuallyObject gets an individual object from the API server
func (m *Matcher) eventuallyObject(obj Object, intervals ...interface{}) gomega.GomegaAsyncAssertion {
	key := types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}
	get := func() Object {
		err := m.Client.Get(context.TODO(), key, obj)
		if err != nil {
			panic(err)
		}
		return obj
	}
	return gomega.Eventually(get, intervals...)
}

// eventuallyList gets a list type  from the API server
func (m *Matcher) eventuallyList(obj runtime.Object, intervals ...interface{}) gomega.GomegaAsyncAssertion {
	list := func() runtime.Object {
		err := m.Client.List(context.TODO(), obj)
		if err != nil {
			panic(err)
		}
		return obj
	}
	return gomega.Eventually(list, intervals...)
}

// WithListItems returns the items of the list
func WithListItems(matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return gomega.WithTransform(func(obj runtime.Object) []runtime.Object {
		items, err := meta.ExtractList(obj)
		if err != nil {
			panic(err)
		}
		return items
	}, matcher)
}

// WithObjectMetaField gets the value of the named field from the Nodes Spec
func WithObjectMetaField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return gomega.WithTransform(func(obj metav1.Object) interface{} {
		r := reflect.ValueOf(obj).MethodByName(fmt.Sprintf("Get%s", field))
		// All Getters take no arguments
		response := r.Call([]reflect.Value{})
		// All Getters return 1 argument
		return response[0].Interface()
	}, matcher)
}

// WithNodeSpecField gets the value of the named field from the Nodes Spec
func WithNodeSpecField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return gomega.WithTransform(func(obj *corev1.Node) interface{} {
		r := reflect.ValueOf(obj.Spec)
		f := reflect.Indirect(r).FieldByName(field)
		return f.Interface()
	}, matcher)
}

// WithNodeRolloutSpecField gets the value of the named field from the
// NodeRollouts Spec
func WithNodeRolloutSpecField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return gomega.WithTransform(func(obj *navarchosv1alpha1.NodeRollout) interface{} {
		r := reflect.ValueOf(obj.Spec)
		f := reflect.Indirect(r).FieldByName(field)
		return f.Interface()
	}, matcher)
}

// WithNodeRolloutStatusField gets the value of the named field from the
// NodeRollouts Status
func WithNodeRolloutStatusField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return gomega.WithTransform(func(obj *navarchosv1alpha1.NodeRollout) interface{} {
		r := reflect.ValueOf(obj.Status)
		f := reflect.Indirect(r).FieldByName(field)
		return f.Interface()
	}, matcher)
}

// WithNodeRolloutConditionField gets the value of the named field from the
// NodeRolloutCondition
func WithNodeRolloutConditionField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return gomega.WithTransform(func(obj navarchosv1alpha1.NodeRolloutCondition) interface{} {
		r := reflect.ValueOf(obj)
		f := reflect.Indirect(r).FieldByName(field)
		return f.Interface()
	}, matcher)
}

// WithNodeReplacementSpecField gets the value of the named field from the
// NodeReplacments Spec
func WithNodeReplacementSpecField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return WithField(fmt.Sprintf("Spec.%s", field), matcher)
}

// WithReplacementSpecField gets the value of the named field from the
// NodeReplacments Spec
func WithReplacementSpecField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return gomega.WithTransform(func(obj navarchosv1alpha1.ReplacementSpec) interface{} {
		r := reflect.ValueOf(obj)
		f := reflect.Indirect(r).FieldByName(field)
		return f.Interface()
	}, matcher)
}

// WithNodeReplacementStatusField gets the value of the named field from the
// NodeReplacements Status
func WithNodeReplacementStatusField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return gomega.WithTransform(func(obj *navarchosv1alpha1.NodeReplacement) interface{} {
		r := reflect.ValueOf(obj.Status)
		f := reflect.Indirect(r).FieldByName(field)
		return f.Interface()
	}, matcher)
}

// WithNodeReplacementConditionField gets the value of the named field from the
// NodeReplacementCondition
func WithNodeReplacementConditionField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return gomega.WithTransform(func(obj navarchosv1alpha1.NodeReplacementCondition) interface{} {
		r := reflect.ValueOf(obj)
		f := reflect.Indirect(r).FieldByName(field)
		return f.Interface()
	}, matcher)
}

// WithTaintField gets the value of the named field from the Taint
func WithTaintField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return gomega.WithTransform(func(obj *corev1.Taint) interface{} {
		r := reflect.ValueOf(obj)
		f := reflect.Indirect(r).FieldByName(field)
		return f.Interface()
	}, matcher)
}

// WithField gets the value of the named field from the object
func WithField(field string, matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	// Addressing Field by <struct>.<field> can be recursed
	fields := strings.SplitN(field, ".", 2)
	if len(fields) == 2 {
		matcher = WithField(fields[1], matcher)
	}

	return gomega.WithTransform(func(obj interface{}) interface{} {
		r := reflect.ValueOf(obj)
		f := reflect.Indirect(r).FieldByName(fields[0])
		if !f.IsValid() {
			panic(fmt.Sprintf("Object '%s' does not have a field '%s'", reflect.TypeOf(obj), fields[0]))
		}
		return f.Interface()
	}, matcher)
}
