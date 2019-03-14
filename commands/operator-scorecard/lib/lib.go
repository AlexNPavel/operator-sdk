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

package lib

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"reflect"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	scorecard "github.com/operator-framework/operator-sdk/commands/operator-sdk/cmd/scorecard"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SimpleScorecardTestConfig contains the user provided config as a yaml byte array
type SimpleScorecardTestConfig struct {
	Config []byte
}

// SimpleScorecardTest is a scorecard plugin test that creates and modifies CRs and checks that the CR's status is updated
// as expected and the expected resources are created correctly
type SimpleScorecardTest struct {
	scorecard.TestInfo
	SimpleScorecardTestConfig
}

// NewSimpleScorecardTest returns a new SimpleScorecardTest object
func NewSimpleScorecardTest(conf SimpleScorecardTestConfig) *SimpleScorecardTest {
	return &SimpleScorecardTest{
		SimpleScorecardTestConfig: conf,
		TestInfo: scorecard.TestInfo{
			Name:        "Simple Scorecard Tests",
			Description: "Simple Scorecard creates and modifies CRs based on a config and checks that the CR's status is properly updated and expected resources are created correctly",
			Cumulative:  false,
		},
	}
}

// UserDefinedTest contains a user defined test. User passes tests as an array using the `functional_tests` viper config
type UserDefinedTest struct {
	// Path to cr to be used for testing
	CRPath string `mapstructure:"cr"`
	// Expected resources and status
	Expected *Expected `mapstructure:"expected"`
	// Sub-tests modifying a few fields with expected changes
	Modifications []Modification `mapstructure:"modifications"`
}

// Expected holds expected resources and status of the CR
type Expected struct {
	// Resources expected to be created after the operator reacts to the CR
	Resources []map[string]interface{} `mapstructure:"resources"`
	// Expected values in CR's status after the operator reacts to the CR
	Status map[string]interface{} `mapstructure:"status"`
}

// Modification specifies a spec field to change in the CR with the expected results
type Modification struct {
	// a map of the spec fields to modify
	Spec map[string]interface{} `mapstructure:"spec"`
	// Expected resources and status
	Expected *Expected `mapstructure:"expected"`
}

var scSuggestions []string
var scTests []scorecard.TestResult

// yamlToUnstructured decodes a yaml file into an unstructured object
func yamlToUnstructured(yamlPath string) (*unstructured.Unstructured, error) {
	yamlFile, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %v", yamlPath, err)
	}
	if bytes.Contains(yamlFile, []byte("\n---\n")) {
		return nil, fmt.Errorf("custom resource manifest cannot have more than 1 resource")
	}
	obj := &unstructured.Unstructured{}
	jsonSpec, err := yaml.YAMLToJSON(yamlFile)
	if err != nil {
		return nil, fmt.Errorf("could not convert yaml file to json: %v", err)
	}
	if err := obj.UnmarshalJSON(jsonSpec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal custom resource manifest to unstructured: %s", err)
	}
	return obj, nil
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
			log.Debugf("%+v != %+v", main, sub)
		}
	}
	if !match {
		// TODO: change this test to make it able to continue checking other values instead of failing fast
		// Disabled since we poll and might trigger this... Will re-enable somehow later
		//scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due nonmatching value for: %s", key))
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
			// Disabled since we poll and might trigger this... Will re-enable somehow later
			//scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due nonmatching value for: %s", key))
			return false
		}
	}
	return true
}

func mapIsSubset(bigMap, subMap map[string]interface{}) bool {
	for key, value := range subMap {
		if _, ok := bigMap[key]; !ok {
			// TODO: make a better way to report what went wrong in functional tests
			// Disabled since we poll and might trigger this... Will re-enable somehow later
			//scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due to missing field in resource: %s", key))
			return false
		}
		if !interfaceChecker(key, bigMap[key], value) {
			// TODO: change this test to make it able to continue checking other values instead of failing fast
			// Disabled since we poll and might trigger this... Will re-enable somehow later
			//scSuggestions = append(scSuggestions, fmt.Sprintf("Functional tests failed due nonmatching value for: %s", key))
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
			// is this case possible or will a slice always be castable to []interface{}?
			return nil, errors.New("slice is not a []interface{}")
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

func updateArray(sourceArr, changes []interface{}) ([]interface{}, error) {
	var err error
	for index, val := range changes {
		sourceArr[index], err = updateVal(sourceArr[index], val)
		if err != nil {
			return nil, err
		}
	}
	return sourceArr, nil
}

func updateMap(sourceMap, changes map[string]interface{}) (map[string]interface{}, error) {
	var err error
	for key, val := range changes {
		sourceMap[key], err = updateVal(sourceMap[key], val)
		if err != nil {
			return nil, fmt.Errorf("error processing key: %s: (%v)", key, err)
		}
	}
	return sourceMap, nil
}

func updateVal(source, change interface{}) (interface{}, error) {
	changeVal := reflect.ValueOf(change)
	sourceVal := reflect.ValueOf(source)
	if source == nil {
		return change, nil
	}
	if changeVal.Kind() == reflect.Map {
		if sourceVal.Kind() != reflect.Map {
			return nil, fmt.Errorf("unmatching types in modifications; type %v != %v", sourceVal.Kind(), changeVal.Kind())
		}
		result, err := updateMap(source.(map[string]interface{}), change.(map[string]interface{}))
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	if changeVal.Kind() == reflect.Slice {
		if sourceVal.Kind() != reflect.Slice {
			return nil, fmt.Errorf("unmatching types in modifications; type %v != %v", sourceVal.Kind(), changeVal.Kind())
		}
		result, err := updateArray(source.([]interface{}), change.([]interface{}))
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	return change, nil
}

func checkResources(resources []map[string]interface{}) (bool, error) {
	for _, res := range resources {
		tempUnstruct := unstructured.Unstructured{Object: res}
		err := wait.Poll(time.Second*1, time.Second*30, func() (bool, error) {
			err := framework.Global.Client.Get(context.TODO(), client.ObjectKey{Namespace: "default", Name: tempUnstruct.GetName()}, &tempUnstruct)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return compareManifests(res, tempUnstruct.Object)
		})
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func checkStatus(status map[string]interface{}, obj *unstructured.Unstructured) (bool, error) {
	objKey, err := client.ObjectKeyFromObject(obj)
	if err != nil {
		return false, err
	}
	err = framework.Global.Client.Get(context.TODO(), objKey, obj)
	if err != nil {
		return false, err
	}
	objStatus, ok := obj.Object["status"].(map[string]interface{})
	if !ok {
		return false, nil
	}
	return compareManifests(status, objStatus)
}

// Run - implemented Test interface
func (t *SimpleScorecardTest) Run(goctx context.Context) *scorecard.TestResult {
	res := &scorecard.TestResult{Test: t}
	userDefinedTests := []UserDefinedTest{}
	err := yaml.Unmarshal(t.Config, &userDefinedTests)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("failed to read config file"))
		return res
	}
	for _, test := range userDefinedTests {
		ctx := framework.NewTestCtx(nil)
		defer ctx.Cleanup()
		err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: time.Second * 10, RetryInterval: time.Second * 1})
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("failed to create namespaced resources: %v", err))
			ctx.Cleanup()
			continue
		}
		res.MaximumPoints += 2
		obj, err := yamlToUnstructured(test.CRPath)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("failed to decode custom resource manifest into object: %s", err))
			ctx.Cleanup()
			continue
		}
		if err := framework.Global.Client.Create(goctx, obj, &framework.CleanupOptions{TestContext: ctx, Timeout: time.Second * 10, RetryInterval: time.Second * 1}); err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("failed to create cr resource: %v", err))
		}
		defer func() {
			err := framework.Global.Client.Delete(goctx, obj)
			if err != nil && !apierrors.IsNotFound(err) {
				log.Errorf("Failed to delete resource type %s: %s, (%v)", obj.GetKind(), obj.GetName(), err)
			}
		}()
		resPass, err := checkResources(test.Expected.Resources)
		if resPass {
			res.EarnedPoints++
		}
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("an error occurred during a resource check: %v", err))
		}
		statPass, err := checkStatus(test.Expected.Status, obj)
		if statPass {
			res.EarnedPoints++
		}
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("an error occurred during a status check: %v", err))
		}
		for _, mod := range test.Modifications {
			res.MaximumPoints += 2
			objKey, err := client.ObjectKeyFromObject(obj)
			if err != nil {
				res.Errors = append(res.Errors, fmt.Errorf("failed to get object key from object: %v", err))
				continue
			}
			err = framework.Global.Client.Get(goctx, objKey, obj)
			obj.Object["spec"], err = updateMap(obj.Object["spec"].(map[string]interface{}), mod.Spec)
			if err != nil {
				res.Errors = append(res.Errors, fmt.Errorf("failed to update CR spec based on config: %v", err))
				continue
			}
			err = framework.Global.Client.Update(goctx, obj)
			if err != nil {
				res.Errors = append(res.Errors, fmt.Errorf("client failed to update CR on server: %v", err))
				continue
			}
			resPass, err := checkResources(mod.Expected.Resources)
			if resPass {
				res.EarnedPoints++
			}
			if err != nil {
				res.Errors = append(res.Errors, fmt.Errorf("an error occured during a resource check: %v", err))
			}
			statPass, err := checkStatus(test.Expected.Status, obj)
			if statPass {
				res.EarnedPoints++
			}
			if err != nil {
				res.Errors = append(res.Errors, fmt.Errorf("an error occured during a status check: %v", err))
			}
		}
		ctx.Cleanup()
	}
	// don't allow 0/0
	if res.MaximumPoints == 0 {
		res.MaximumPoints = 1
	}
	return res
}
