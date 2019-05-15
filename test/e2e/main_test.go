// Copyright 2018 The Operator-SDK Authors
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

package e2e

import (
	"flag"
	"testing"

	test "github.com/operator-framework/operator-sdk/internal/test"
	f "github.com/operator-framework/operator-sdk/pkg/test"
)

var (
	e2eImageName *string
	noImageBuild *bool
	openshiftCI  *bool
)

func TestMain(m *testing.M) {
	e2eImageName = flag.String("image", "", "operator image name <repository>:<tag> used to push the image, defaults to none (builds image to local docker repo)")
	test.OnlyGenerate = flag.Bool("generate-only", false, "only generate the project (used for multi-stage build test)")
	noImageBuild = flag.Bool("no-image-build", false, "do not build the image during the test (used for multu-stage build test)")
	f.MainEntry(m)
}
