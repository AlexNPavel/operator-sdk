// Copyright 2019 The Operator-SDK Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scorecard

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// Struct containing a user defined test. User passes tests as an array using the `functional_tests` viper config
type UserDefinedTest struct {
	// Path to cr to be used for testing
	CRPath string `mapstructure:"cr"`
	// Expected resources and status
	Expected Expected `mapstructure:"expected"`
	// Sub-tests modifying a few fields with expected changes
	Modifications []Modification `mapstructure:"modifications"`
}

type Expected struct {
	// Resources expected to be created after the operator reacts to the CR
	Resources []ExpectedResource `mapstructure:"resources"`
	// Expected values in CR's status after the operator reacts to the CR
	Status map[string]interface{} `mapstructure:"status"`
}

// Struct containing a resource and its expected fields
type ExpectedResource struct {
	// (if set) Namespace of resource
	Namespace string `mapstructure:"namespace"`
	// APIVersion of resource
	APIVersion string `mapstructure:"apiversion"`
	// Kind of resource
	Kind string `mapstructure:"kind"`
	// Name of resource
	Name string `mapstructure:"name"`
	// The fields we expect to see in this resource
	Fields map[string]interface{} `mapstructure:"fields"`
}

// Modifications specifies a spec field to change in the CR with the expected results
type Modification struct {
	// a map of the spec fields to modify
	Spec map[string]interface{} `mapstructure:"spec"`
	// Expected resources and status
	Expected Expected `mapstructure:"expected"`
}

// castToMap takes an interface and attempts to convert it to
// a map[string]interface{}. This is necessary because maps
// under the top level of a YAML map are map[interface{}]interface{}
// and this will not be fixed until at least go-yaml v3.
// https://github.com/go-yaml/yaml/pull/385
func castToMap(i interface{}) (map[string]interface{}, error) {
	retVal := make(map[string]interface{})
	origMap, ok := i.(map[interface{}]interface{})
	if !ok {
		origMap, ok := i.(map[string]interface{})
		if ok {
			return origMap, nil
		}
		return nil, errors.New("Interface is not a map[interface{}]interface{}")
	}
	for key, val := range origMap {
		stringKey, ok := key.(string)
		if !ok {
			return nil, errors.New("Map key is not string")
		}
		retVal[stringKey] = val
	}
	return retVal, nil
}

func isNum(i interface{}) bool {
	switch reflect.ValueOf(i).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func convertToFloat64(i interface{}) (float64, error) {
	valueOf := reflect.ValueOf(i)
	switch valueOf.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(valueOf.Int()), nil
	case reflect.Float32, reflect.Float64:
		return valueOf.Float(), nil
	default:
		return 0, fmt.Errorf("Type %s is not a number", valueOf.Kind())
	}
}

func interfaceChecker(key string, main, sub interface{}) bool {
	mainValue := reflect.ValueOf(main)
	subValue := reflect.ValueOf(sub)
	match := true
	switch subValue.Kind() {
	case reflect.Slice:
		switch mainValue.Kind() {
		case reflect.Slice:
			match = sliceIsSubset(key, main.([]interface{}), sub.([]interface{}))
		default:
			log.Infof("In slice mismatch case for key %s", key)
			scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due mismatched type for: %s", key))
			return false
		}
	case reflect.Map:
		switch mainValue.Kind() {
		case reflect.Map:
			mainMap, err := castToMap(main)
			if err != nil {
				return false
			}
			subMap, err := castToMap(sub)
			if err != nil {
				return false
			}
			match = mapIsSubset(mainMap, subMap)
		default:
			log.Infof("In map mismatch case for key %s", key)
			scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due mismatched type for: %s", key))
			return false
		}
	default:
		log.Infof("MainKind: %s", mainValue.Kind())
		log.Infof("SubKind: %s", subValue.Kind())
		// Sometimes, one parser will decide a number is an int while the another parser says float
		// For these cases, we cast both numbers to floats
		nums := false
		var num1, num2 float64
		if isNum(main) && isNum(sub) {
			var err error
			nums = true
			num1, err = convertToFloat64(main)
			if err != nil {
				// TODO: handle this situation differently...
				return false
			}
			num2, err = convertToFloat64(sub)
			if err != nil {
				// TODO: handle this situation differently...
				return false
			}
		}
		if nums {
			match = (num1 == num2)
		} else {
			match = reflect.DeepEqual(main, sub)
		}
		if !match {
			log.Infof("%+v != %+v", main, sub)
		}
	}
	if !match {
		log.Infof("In the case for key %s", key)
		// TODO: change this test to make it able to continue checking other values instead of failing fast
		scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due nonmatching value for: %s", key))
	}
	return match
}

func sliceIsSubset(key string, bigSlice, subSlice []interface{}) bool {
	if len(subSlice) > len(bigSlice) {
		return false
	}
	for index, value := range subSlice {
		if !interfaceChecker(key, bigSlice[index], value) {
			// TODO: change this test to make it able to continue checking other values instead of failing fast
			scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due nonmatching value for: %s", key))
			return false
		}
	}
	return true
}

func mapIsSubset(bigMap, subMap map[string]interface{}) bool {
	for key, value := range subMap {
		if _, ok := bigMap[key]; !ok {
			// TODO: make a better way to report what went wrong in functional tests
			scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due to missing field in resource: %s", key))
			return false
		}
		if !interfaceChecker(key, bigMap[key], value) {
			// TODO: change this test to make it able to continue checking other values instead of failing fast
			scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due nonmatching value for: %s", key))
			return false
		}
	}
	return true
}

func scorecareFunctionLength(expected, actual interface{}) bool {
	expectedSliceLength, ok := expected.(int)
	if !ok {
		return false
	}
	actualSlice, ok := actual.([]interface{})
	if !ok {
		return false
	}
	return expectedSliceLength == len(actualSlice)
}

func getMatchingLeavesMap(config, manifest map[string]interface{}) ([]map[string]interface{}, error) {
	retVal := make([]map[string]interface{}, 1, 1)
	for key, val := range config {
		if manifest[key] == nil {
			return nil, nil
		}
		newLeaves, err := getMatchingLeaves(val, manifest[key])
		if err != nil {
			return retVal, err
		}
		if newLeaves == nil {
			newMap := make(map[string]interface{})
			newMap["config"] = val
			newMap["manifest"] = manifest[key]
			retVal = append(retVal, newMap)
		} else {
			retVal = append(retVal, newLeaves...)
		}
	}
	return retVal, nil
}

// getMatchingLeavesArray is the same as getMatchingLeaves but handles arrays instead of maps
func getMatchingLeavesArray(config, manifest []interface{}) ([]map[string]interface{}, error) {
	retVal := make([]map[string]interface{}, 1, 1)
	for index, val := range config {
		newLeaves, err := getMatchingLeaves(val, manifest[index])
		if err != nil {
			return retVal, err
		}
		if newLeaves == nil {
			newMap := make(map[string]interface{})
			newMap["config"] = val
			newMap["manifest"] = manifest[index]
			retVal = append(retVal, newMap)
		} else {
			retVal = append(retVal, newLeaves...)
		}
	}
	return retVal, nil
}

// getMatchingLeaves walks to the end of the config map and gets the field that that corresponds to in the manifest map
// Used for scorecard functions. Returns an array of maps in this form, where each array element is one leaf:
// {
//	config:
//		configValInterface
//	manifest:
//		manifestValInterface
// }
func getMatchingLeaves(config, manifest interface{}) ([]map[string]interface{}, error) {
	if reflect.ValueOf(config).Kind() == reflect.Map {
		configMap, err := castToMap(config)
		if err != nil {
			return nil, err
		}
		manMap, err := castToMap(manifest)
		if err != nil {
			return nil, err
		}
		return getMatchingLeavesMap(configMap, manMap)
	}
	if reflect.ValueOf(config).Kind() == reflect.Slice {
		configSlice, ok := config.([]interface{})
		if !ok {
			return nil, errors.New("Wat")
		}
		manSlice, ok := manifest.([]interface{})
		if !ok {
			return nil, errors.New("manifest field does not match config field")
		}
		return getMatchingLeavesArray(configSlice, manSlice)
	}
	return nil, nil
}

func runScorecardFunction(expected, actual map[string]interface{}, function func(expected, actual interface{}) bool) bool {
	results, err := getMatchingLeavesMap(expected, actual)
	if err != nil {
		log.Fatalf("Failed: %s", err)
	}
	// trim empty maps
	for _, item := range results {
		if len(item) != 0 && !function(item["config"], item["manifest"]) {
			return false
		}
	}
	return true
}

// compareManifests uses the config to verify that a manifest meets requirements specified by config
func compareManifests(config, manifest map[string]interface{}) (bool, error) {
	pass := true
	log.Infof("Running for %+v", config)
	for key, val := range config {
		log.Infof("Running for %+v", val)
		valMap, err := castToMap(val)
		if err != nil {
			log.Infof("Failed in valmap: %s", err)
			return false, nil
		}
		if strings.HasPrefix(key, "scorecard_function_") {
			log.Infof("Have prefix")
			switch strings.TrimPrefix(key, "scorecard_function_") {
			case "length":
				log.Infof("Running scorecard function length")
				if !runScorecardFunction(valMap, manifest, scorecareFunctionLength) {
					pass = false
				}
			default:
				// incorrectly defined scorecard function
				pass = false
			}
			continue
		}
		log.Infof("Running for %+v", manifest[key])
		manKeyMap, err := castToMap(manifest[key])
		if err != nil {
			log.Infof("Failed in manmap: %s", err)
			return false, nil
		}
		if !mapIsSubset(manKeyMap, valMap) {
			pass = false
		}
	}
	return pass, nil
}

func userDefinedTests() error {
	userDefinedTests := []UserDefinedTest{}
	if !viper.IsSet("functional_tests") {
		return errors.New("functional_tests config not set")
	}
	err := viper.UnmarshalKey("functional_tests", &userDefinedTests)
	if err != nil {
		return err
	}
	// use userdefinedTests as sub and make a custom map to test against
	depYAMLUnmarshalled := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(depYAML), &depYAMLUnmarshalled)
	if err != nil {
		return err
	}
	log.Info(fmt.Sprintf("Is Dep Pass? %t", mapIsSubset(depYAMLUnmarshalled, userDefinedTests[0].Expected.Resources[0].Fields)))
	statusYAMLUnmarshalled := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(statusYAML), &statusYAMLUnmarshalled)
	if err != nil {
		return err
	}
	results, err := compareManifests(userDefinedTests[0].Expected.Status, statusYAMLUnmarshalled)
	log.Infof("Ran compare manifests")
	if err != nil {
		return err
	}
	log.Info(fmt.Sprintf("Is Status Pass? %+v", results))
	log.Info("Suggestions: %+v", scSuggestions)
	log.Info("Trying to get dep")
	unstruct := unstructured.Unstructured{}
	unstruct.SetUnstructuredContent(depYAMLUnmarshalled)
	log.Infof("Namespace: %s", unstruct.GetNamespace())
	log.Infof("Name: %s", unstruct.GetName())
	recvUnstruct := unstructured.Unstructured{}
	recvUnstruct.SetGroupVersionKind(unstruct.GroupVersionKind())
	err = runtimeClient.Get(context.TODO(), client.ObjectKey{Namespace: unstruct.GetNamespace(), Name: unstruct.GetName()}, &recvUnstruct)
	if err != nil {
		return err
	}
	log.Info(fmt.Sprintf("Is Dep Pass v2? %t", mapIsSubset(recvUnstruct.Object, userDefinedTests[0].Expected.Resources[0].Fields)))
	return nil
}

const depYAML = `apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  annotations:
    deployment.kubernetes.io/revision: "1"
  creationTimestamp: 2019-02-04T22:21:21Z
  generation: 1
  name: example-memcached
  namespace: default
  ownerReferences:
  - apiVersion: cache.example.com/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: Memcached
    name: example-memcached
    uid: 33039fc7-28cb-11e9-9ade-3438e02bae33
  resourceVersion: "35888"
  selfLink: /apis/extensions/v1beta1/namespaces/default/deployments/example-memcached
  uid: 33080a9d-28cb-11e9-9ade-3438e02bae33
spec:
  progressDeadlineSeconds: 600
  replicas: 3
  revisionHistoryLimit: 10
  selector:
    matchLabels:
      app: memcached
      memcached_cr: example-memcached
  strategy:
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 25%
    type: RollingUpdate
  template:
    metadata:
      creationTimestamp: null
      labels:
        app: memcached
        memcached_cr: example-memcached
    spec:
      containers:
      - command:
        - memcached
        - -m=64
        - -o
        - modern
        - -v
        image: memcached:1.4.36-alpine
        imagePullPolicy: IfNotPresent
        name: memcached
        ports:
        - containerPort: 11211
          name: memcached
          protocol: TCP
        resources: {}
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
status:
  availableReplicas: 3
  conditions:
  - lastTransitionTime: 2019-02-04T22:21:24Z
    lastUpdateTime: 2019-02-04T22:21:24Z
    message: Deployment has minimum availability.
    reason: MinimumReplicasAvailable
    status: "True"
    type: Available
  - lastTransitionTime: 2019-02-04T22:21:21Z
    lastUpdateTime: 2019-02-04T22:21:24Z
    message: ReplicaSet "example-memcached-55dc4795d6" has successfully progressed.
    reason: NewReplicaSetAvailable
    status: "True"
    type: Progressing
  observedGeneration: 1
  readyReplicas: 3
  replicas: 3
  updatedReplicas: 3
`

const statusYAML = `nodes:
  - example-memcached-55dc4795d6-ggl5q
  - example-memcached-55dc4795d6-jxbvr
  - example-memcached-55dc4795d6-xxz55
`
