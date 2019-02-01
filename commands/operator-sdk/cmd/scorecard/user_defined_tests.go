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
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ghodss/yaml"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// Struct containing a user defined test. User passes tests as an array using the `functional_tests` viper config
type UserDefinedTest struct {
	// Path to cr to be used for testing
	CRPath string `mapstructure:"cr"`
	// Resources expected to be created after the operator reacts to the CR
	ExpectedResources []ExpectedResource `mapstructure:"expected_resources"`
	// Expected values in CR's status after the operator reacts to the CR
	ExpectedStatus map[string]interface{} `mapstructure:"expected_status"`
	// Sub-tests modifying a few fields with expected changes
	Modifications []Modification `mapstructure:"modifications"`
}

// Struct containing a resource and its expected fields
type ExpectedResource struct {
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
	// the resources we expect to be created after the spec fields are modified
	ExpectedResources []ExpectedResource `mapstructure:"expected_resources"`
	// the status we expect to see after the spec fields are modified
	ExpectedStatus map[string]interface{} `mapstructure:"expected_status"`
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
			scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due mismatched type for: %s", key))
			return false
		}
	default:
		if !reflect.DeepEqual(main, sub) {
			match = false
		}
	}
	if !match {
		// TODO: change this test to make it able to continue checking other values instead of failing fast
		scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due nonmatching value for: %s", key))
		return false
	}
	return true
}

func sliceIsSubset(key string, bigSlice, subSlice []interface{}) bool {
	if len(subSlice) > len(bigSlice) {
		return false
	}
	for index, value := range subSlice {
		if interfaceChecker(key, bigSlice[index], value) {
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
		if interfaceChecker(key, bigMap[key], value) {
			// TODO: change this test to make it able to continue checking other values instead of failing fast
			scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due nonmatching value for: %s", key))
			return false
		}
	}
	return true
}

func scorecareFunctionLength(expected, actual interface{}) bool {
	expectedSlice, ok := expected.([]interface{})
	if !ok {
		return false
	}
	actualSlice, ok := actual.([]interface{})
	if !ok {
		return false
	}
	return len(expectedSlice) == len(actualSlice)
}

// getMatchingLeaves walks to the end of the config map and gets the field that that corresponds to in the manifest map
// Used for scorecard functions. Returns an array of maps in this form, where each array element is one leaf:
// {
//	config:
//		configValInterface
//	manifest:
//		manifestValInterface
// }
func getMatchingLeaves(config, manifest map[string]interface{}) ([]map[string]interface{}, error) {
	retVal := make([]map[string]interface{}, 1, 1)
	for key, val := range config {
		if reflect.ValueOf(val).Kind() == reflect.Map {
			valMap, err := castToMap(val)
			if err != nil {
				return nil, err
			}
			manKeyMap, err := castToMap(manifest[key])
			if err != nil {
				return nil, err
			}
			newLeaves, err := getMatchingLeaves(valMap, manKeyMap)
			if err != nil {
				return nil, err
			}
			retVal = append(retVal, newLeaves...)
		}
	}
	return retVal, nil
}

// getMatchingLeavesArray is the same as getMatchingLeaves but handles arrays instead of maps
func getMatchingLeavesArray(config, manifest []interface{}) ([]map[string]interface{}, error) {
	retVal := make([]map[string]interface{}, 1, 1)
	for index, val := range config {

	}
	return nil, nil
}

func getMatchingLeavesParser(config, manifest interface{}) ([]map[string]interface{}, error) {
	if reflect.ValueOf(config).Kind() == reflect.Map {
		configMap, err := castToMap(config)
		if err != nil {
			return nil, err
		}
		manMap, err := castToMap(manifest)
		if err != nil {
			return nil, err
		}
		return getMatchingLeaves(configMap, manMap)
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
	return false
}

// compareManifests uses the config to verify that a manifest meets requirements specified by config
func compareManifests(config, manifest map[string]interface{}) (bool, error) {
	pass := true
	for key, val := range config {
		valMap, err := castToMap(val)
		if err != nil {
			return false, nil
		}
		manKeyMap, err := castToMap(manifest[key])
		if err != nil {
			return false, nil
		}
		if strings.HasPrefix(key, "scorecard_function_") {
			switch strings.TrimPrefix(key, "scorecard_function_") {
			case "length":
				if !runScorecardFunction(valMap, manifest, scorecareFunctionLength) {
					pass = false
				}
			default:
				// incorrectly defined scorecard function
				pass = false
			}
			continue
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
	log.Info(fmt.Sprintf("Is Dep Pass? %t", mapIsSubset(depYAMLUnmarshalled, userDefinedTests[0].ExpectedResources[0].Fields)))
	statusYAMLUnmarshalled := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(statusYAML), &statusYAMLUnmarshalled)
	if err != nil {
		return err
	}
	log.Info(fmt.Sprintf("Is Status Pass? %t", mapIsSubset(statusYAMLUnmarshalled, userDefinedTests[0].ExpectedStatus)))
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
        image: memcached:1.4.36-alpinle
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

const statusYAML = `apiVersion: cache.example.com/v1alpha1
kind: Memcached
metadata:
  creationTimestamp: 2019-02-04T23:18:40Z
  generation: 1
  name: example-memcached
  namespace: default
  resourceVersion: "40528"
  selfLink: /apis/cache.example.com/v1alpha1/namespaces/default/memcacheds/example-memcached
  uid: 351f7078-28d3-11e9-9ade-3438e02bae33
spec:
  size: 3
status:
  nodes:
  - example-memcached-55dc4795d6-ggl5q
  - example-memcached-55dc4795d6-jxbvr
  - example-memcached-55dc4795d6-xxz55
`
