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

package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/operator-framework/operator-sdk/commands/operator-scorecard/lib"
	"github.com/operator-framework/operator-sdk/internal/util/yamlutil"
	"github.com/operator-framework/operator-sdk/pkg/scaffold"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/test/test-framework/pkg/apis"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

var (
	configPath     string
	kubeconfigPath string
	namespace      string
	namespacedPath string
	globalPath     string
)

func getCRDGVKs(yamlFile []byte) ([]schema.GroupVersionKind, error) {
	var gvks []schema.GroupVersionKind

	scanner := yamlutil.NewYAMLScanner(yamlFile)
	for scanner.Scan() {
		yamlSpec := scanner.Bytes()

		obj := &unstructured.Unstructured{}
		jsonSpec, err := yaml.YAMLToJSON(yamlSpec)
		if err != nil {
			return nil, fmt.Errorf("could not convert yaml file to json: %v", err)
		}
		if err := obj.UnmarshalJSON(jsonSpec); err != nil {
			return nil, fmt.Errorf("failed to unmarshal object spec: (%v)", err)
		}
		if obj.GetKind() != "CustomResourceDefinition" {
			continue
		}
		spec, ok := obj.Object["spec"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("could not get spec for CRD %s", obj.GetName())
		}
		version, ok := spec["version"].(string)
		if !ok || version == "" {
			versions, ok := spec["versions"].([]string)
			if !ok {
				return nil, fmt.Errorf("could not get version for CRD %s", obj.GetName())
			}
			if len(versions) == 0 {
				return nil, fmt.Errorf("could not get versions for CRD %s", obj.GetName())

			}
			version = versions[0]
		}
		names, ok := spec["names"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("could not get names for CRD %s", obj.GetName())
		}
		kind, ok := names["kind"].(string)
		if !ok {
			return nil, fmt.Errorf("could not get kind for CRD %s", obj.GetName())
		}
		group, ok := spec["group"].(string)
		if !ok {
			return nil, fmt.Errorf("could not get group for CRD %s", obj.GetName())
		}
		gvks = append(gvks, schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
	}
	return gvks, nil
}

func main() {
	flag.StringVar(&configPath, "config", "", "path to config file")
	flag.StringVar(&kubeconfigPath, "kubeconfig", "", "path to kubeconfig")
	flag.StringVar(&namespace, "namespace", "", "namespace to run in")
	flag.StringVar(&namespacedPath, "namespacedManifest", "", "path to namespaced manifest")
	flag.StringVar(&globalPath, "globalManifest", "", "path to global manifest")
	flag.Parse()
	if configPath == "" {
		var ok bool
		configPath, ok = os.LookupEnv("SIMPLE_SCORECARD_CONFIG")
		if !ok {
			log.Fatal("config path must be set via the config flag or SIMPLE_SCORECARD_CONFIG env var")
		}
	}
	log.Printf("config path: %s", configPath)
	yamlSpecs, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Fatalf("failed to read config file %s: %v", configPath, err)
	}
	if namespacedPath == "" {
		namespacedManifest, err := yamlutil.GenerateCombinedNamespacedManifest(scaffold.DeployDir)
		if err != nil {
			log.Fatalf("could not generate namespaced manifest file: %v", err)
		}
		namespacedPath = namespacedManifest.Name()
	}
	if err := framework.Setup(kubeconfigPath, namespacedPath, namespace, false); err != nil {
		log.Fatalf("Failed to set up framework: %v", err)
	}
	// setup context to use when setting up crd
	ctx := framework.NewTestCtx(nil)
	defer ctx.Cleanup()
	if globalPath == "" {
		globalManifest, err := yamlutil.GenerateCombinedGlobalManifest(scaffold.CRDsDir)
		if err != nil {
			log.Fatalf("could not generate global manifest file: %v", err)
		}
		globalPath = globalManifest.Name()
	}
	gManifestBytes, err := ioutil.ReadFile(globalPath)
	if err != nil {
		log.Fatalf("failed to read global manifest file %s: %v", configPath, err)
	}
	err = ctx.CreateFromYAML(gManifestBytes, true, &framework.CleanupOptions{TestContext: ctx, Timeout: time.Second * 10, RetryInterval: time.Second * 1})
	if err != nil {
		log.Fatalf("could not create global resources: %v", err)
	}
	gvks, err := getCRDGVKs(gManifestBytes)
	if err != nil {
		log.Fatalf("%v", err)
	}
	for _, gvk := range gvks {
		objList := &unstructured.UnstructuredList{}
		objList.SetGroupVersionKind(gvk)
		err = framework.AddToFrameworkScheme(apis.AddToScheme, objList)
		if err != nil {
			log.Fatalf("Failed to add custom resource scheme to framework: %v", err)
		}
	}
	test := lib.NewSimpleScorecardTest(lib.SimpleScorecardTestConfig{Config: yamlSpecs})
	results := test.Run(context.TODO())
	fmt.Printf("\n%+v\n", results)
}
