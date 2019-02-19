package scorecard

import (
	"testing"

	"github.com/ghodss/yaml"
)

func TestUserDefinedTests(t *testing.T) {
	userDefinedTests := []UserDefinedTest{}
	err := yaml.Unmarshal([]byte(userDefinedTestsConfig), &userDefinedTests)
	if err != nil {
		t.Fatalf("could not unmarshal config: %v", err)
	}
	depYAMLUnmarshalled := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(userDefinedTestsDepYAML), &depYAMLUnmarshalled)
	if err != nil {
		t.Fatalf("could not unmarshal dep: %v", err)
	}
	depPass, err := compareManifests(userDefinedTests[0].Expected.Resources[0], depYAMLUnmarshalled)
	if !depPass {
		t.Errorf("dep pass failed")
	}
	if err != nil {
		t.Errorf("dep pass error: %v", err)
	}
	statusYAMLUnmarshalled := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(userDefinedTestsStatusYAML), &statusYAMLUnmarshalled)
	if err != nil {
		t.Fatalf("could not unmarshal status: %v", err)
	}
	statusPass, err := compareManifests(userDefinedTests[0].Expected.Status, statusYAMLUnmarshalled)
	if !statusPass {
		t.Errorf("status pass failed")
	}
	if err != nil {
		t.Errorf("status pass error: %v", err)
	}
}

const userDefinedTestsConfig = `- cr: "deploy/crds/cache_v1alpha1_memcached_cr.yaml"
  expected:
    resources:
    - apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: example-memcached
      status:
        readyReplicas: 3
      spec:
        template:
          spec:
            containers:
              - image: memcached:1.4.36-alpine
    status:
    scorecard_function_length:
      nodes: 3
`

const userDefinedTestsDepYAML = `apiVersion: apps/v1
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

const userDefinedTestsStatusYAML = `nodes:
  - example-memcached-55dc4795d6-ggl5q
  - example-memcached-55dc4795d6-jxbvr
  - example-memcached-55dc4795d6-xxz55
`
