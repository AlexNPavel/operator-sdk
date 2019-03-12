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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/operator-framework/operator-sdk/commands/operator-scorecard/lib"
)

var configPath string
var kubeconfigPath string
var namespace string

func main() {
	flag.StringVar(&configPath, "config", "", "path to config file")
	flag.StringVar(&kubeconfigPath, "kubeconfig", "", "path to kubeconfig")
	flag.StringVar(&namespace, "namespace", "", "namespace to run in")
	flag.Parse()
	if configPath == "" {
		var ok bool
		configPath, ok = os.LookupEnv("SIMPLE_SCORECARD_CONFIG")
		if !ok {
			log.Fatal("config path must be set via the config flag or SIMPLE_SCORECARD_CONFIG env var")
		}
	}
	if err := framework.setup(kubeconfigPath, "", false); err != nil {
		log.Fatalf("Failed to set up framework: %v", err)
	}
	if namespace == "" {
		namespace = "l"
	}
	log.Printf("config path: %s", configPath)
	yamlSpecs, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %v", yamlPath, err)
	}
	// setup context to use when setting up crd
	ctx := NewTestCtx(nil)
	err := lib.UserDefinedTests(yamlSpecs)
	if err != nil {
		os.Exit(1)
	}
}
