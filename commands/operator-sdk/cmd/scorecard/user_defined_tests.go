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
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/ghodss/yaml"
	goyaml "gopkg.in/yaml.v2"
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
	Expected *Expected `mapstructure:"expected"`
	// Sub-tests modifying a few fields with expected changes
	Modifications []Modification `mapstructure:"modifications"`
}

type Expected struct {
	// Resources expected to be created after the operator reacts to the CR
	Resources []map[string]interface{} `mapstructure:"resources"`
	// Expected values in CR's status after the operator reacts to the CR
	Status map[string]interface{} `mapstructure:"status"`
}

// Modifications specifies a spec field to change in the CR with the expected results
type Modification struct {
	// a map of the spec fields to modify
	Spec map[string]interface{} `mapstructure:"spec"`
	// Expected resources and status
	Expected *Expected `mapstructure:"expected"`
}

func fixExpected(exp *Expected) (*Expected, error) {
	fixedResources := make([]map[string]interface{}, 0, 0)
	for _, item := range exp.Resources {
		marshaled, err := goyaml.Marshal(item)
		if err != nil {
			return nil, err
		}
		strMap := make(map[string]interface{})
		err = yaml.Unmarshal(marshaled, &strMap)
		if err != nil {
			return nil, err
		}
		fixedResources = append(fixedResources, strMap)
	}
	marshaledStat, err := goyaml.Marshal(exp.Status)
	if err != nil {
		return nil, err
	}
	fixedStat := make(map[string]interface{})
	err = yaml.Unmarshal(marshaledStat, &fixedStat)
	if err != nil {
		return nil, err
	}
	return &Expected{Resources: fixedResources, Status: fixedStat}, nil
}

func fixModifications(modifications []Modification) ([]Modification, error) {
	var fixedModifications []Modification
	for _, mod := range modifications {
		fixedMod := Modification{}
		marshaledSpec, err := goyaml.Marshal(mod.Spec)
		if err != nil {
			return nil, err
		}
		fixedSpec := make(map[string]interface{})
		err = yaml.Unmarshal(marshaledSpec, &fixedSpec)
		if err != nil {
			return nil, err
		}
		fixedMod.Spec = fixedSpec
		fixedExpected, err := fixExpected(mod.Expected)
		if err != nil {
			return nil, err
		}
		fixedMod.Expected = fixedExpected
		fixedModifications = append(fixedModifications, fixedMod)
	}
	return fixedModifications, nil
}

// fixMaps converts YAML style maps (whose keys can be interfaces) to
// JSON style maps (whose keys can only be strings). This fixes many
// compatibility issues for us when working with kubernetes. This
// may be possible directly with go-yaml in the future:
// https://github.com/go-yaml/yaml/pull/385
func fixMaps(tests []UserDefinedTest) ([]UserDefinedTest, error) {
	var fixedTests []UserDefinedTest
	for _, test := range tests {
		fixedTest := UserDefinedTest{CRPath: test.CRPath}
		fixedExpected, err := fixExpected(test.Expected)
		if err != nil {
			return nil, err
		}
		fixedTest.Expected = fixedExpected
		fixedModifications, err := fixModifications(test.Modifications)
		if err != nil {
			return nil, err
		}
		fixedTest.Modifications = fixedModifications
		fixedTests = append(fixedTests, fixedTest)
	}
	return fixedTests, nil
}

func tryConvertToFloat64(i interface{}) (float64, bool) {
	valueOf := reflect.ValueOf(i)
	switch valueOf.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(valueOf.Int()), true
	case reflect.Float32, reflect.Float64:
		return valueOf.Float(), true
	default:
		return 0, false
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
			scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due mismatched type for: %s", key))
			return false
		}
	case reflect.Map:
		switch mainValue.Kind() {
		case reflect.Map:
			match = mapIsSubset(main.(map[string]interface{}), sub.(map[string]interface{}))
		default:
			scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due mismatched type for: %s", key))
			return false
		}
	default:
		// Sometimes, one parser will decide a number is an int while the another parser says float
		// For these cases, we cast both numbers to floats
		num1, bool1 := tryConvertToFloat64(main)
		num2, bool2 := tryConvertToFloat64(sub)
		if bool1 && bool2 {
			match = (num1 == num2)
		} else {
			match = reflect.DeepEqual(main, sub)
		}
		if !match {
			log.Infof("%+v != %+v", main, sub)
		}
	}
	if !match {
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
	// all ints from ghodss unmarshaled YAML are int64 or float64
	expectedSliceLength, ok := expected.(float64)
	if !ok {
		expectedSliceLengthInt, ok := expected.(int64)
		if !ok {
			return false
		}
		expectedSliceLength = float64(expectedSliceLengthInt)
	}
	actualSlice, ok := actual.([]interface{})
	if !ok {
		return false
	}
	return expectedSliceLength == float64(len(actualSlice))
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
		return getMatchingLeavesMap(config.(map[string]interface{}), manifest.(map[string]interface{}))
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
	// separate scorecard_function_ fields from rest of object
	for key, val := range config {
		if strings.HasPrefix(key, "scorecard_function_") {
			delete(config, key)
			switch strings.TrimPrefix(key, "scorecard_function_") {
			case "length":
				if !runScorecardFunction(val.(map[string]interface{}), manifest, scorecareFunctionLength) {
					pass = false
				}
			default:
				// incorrectly defined scorecard function
				pass = false
			}
			continue
		}
	}
	if !mapIsSubset(manifest, config) {
		pass = false
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
	userDefinedTests, err = fixMaps(userDefinedTests)
	if err != nil {
		return err
	}
	// use userdefinedTests as sub and make a custom map to test against
	depYAMLUnmarshalled := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(depYAML), &depYAMLUnmarshalled)
	if err != nil {
		return err
	}
	depPass, err := compareManifests(userDefinedTests[0].Expected.Resources[0], depYAMLUnmarshalled)
	log.Info(fmt.Sprintf("Is Dep Pass? %t", depPass))
	if !depPass || err != nil {
		return fmt.Errorf("Err: %s", err)
	}
	statusYAMLUnmarshalled := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(statusYAML), &statusYAMLUnmarshalled)
	if err != nil {
		return err
	}
	results, err := compareManifests(userDefinedTests[0].Expected.Status, statusYAMLUnmarshalled)
	if err != nil {
		return err
	}
	log.Info(fmt.Sprintf("Is Status Pass? %+v", results))
	resource1 := userDefinedTests[0].Expected.Resources[0]
	tempUnstruct := unstructured.Unstructured{Object: resource1}
	if err := createFromYAMLFile(userDefinedTests[0].CRPath); err != nil {
		return fmt.Errorf("failed to create cr resource: %v", err)
	}
	obj, err := yamlToUnstructured(userDefinedTests[0].CRPath)
	if err != nil {
		return fmt.Errorf("failed to decode custom resource manifest into object: %s", err)
	}
	err = wait.Poll(time.Second*1, time.Second*30, func() (bool, error) {
		err = runtimeClient.Get(context.TODO(), client.ObjectKey{Namespace: "default", Name: tempUnstruct.GetName()}, &tempUnstruct)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return compareManifests(userDefinedTests[0].Expected.Resources[0], tempUnstruct.Object)
	})
	if err != nil {
		return err
	}
	log.Info("Passed!")
	err = runtimeClient.Delete(context.TODO(), obj)
	if err != nil {
		return err
	}
	return fmt.Errorf("STOP")
}

const depYAML = `apiVersion: apps/v1
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
