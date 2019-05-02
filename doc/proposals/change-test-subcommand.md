# Change or Remove Test Subcommand

Implementation Owner: @AlexNPavel
Status: Draft

[Background](#Background)
[Goals](#Goals)
[Design overview](#Design_overview)
[User facing usage](#User_facing_usage)
[Observations and open questions](#Observations_and_open_questions)

## Background

The operator-sdk has a test framework for operators written in golang and the sdk binary provides some subcommands to assist in running these tests.
The SDK has supported the `test` subcommand since `v0.0.6` and split the subcommand into 2 modes in `v0.0.7`: `local` and `incluster`. `local`
runs the `go test` binary locally on the developers machine and `incluster` instead uses a special operator image with built in tests (which
can be built using `operator-sdk build --enable-tests`) and runs the go binary from within the kubernetes cluster. The `local` test mode is
expected to be run with admin permissions and can create all resources it needs (including multiple namespaces for parallel testing) while
the `incluster` mode is more limited and can only run in a single namespace and requires the global and RBAC resources to be created before running
the tests.

Since the test subcommand was introduced, the `operator-sdk scorecard` subcommand was introduced, and a modular plugin system will be introduced to the
SDK soon. This new system can allow the SDK to remove the `test` subcommand from the binary itself and instead implement it as a plugin.

Over time, we also noticed that very few, if anybody, is using the `incluster` testing mode. To reduce maintenance overhead and reduce the risk of bugs,
it may make sense to deprecate and remove the `incluster` testing mode from the SDK.

## Goals

- Deprecate and remove the `test incluster` subcommand
- Remove `test local` subcommand and reimplement is as a scorecard plugin

## Description of current test subcommand implementation

### Local

The `local` subcommand is extremely configurable and has many flags and configuration options that have been added as a result of requests from the community and
supports running tests built using the sdk's test framework for Go or Ansuble Molecule. For the Ansible Molecule tests, there is a flag to simply pass flags to
Molecule itself. For the Go based test framework, we have the following options:

- `--global-manifest` - Path to global resources such as CRDs (defaults to all CRDs in `deploy/crds`)
- `--namespaced-manifest` - Path to namespaced resources (defaults to all resources in `deploy/{service_account.yaml,role.yaml,role_binding.yaml,operator.yaml}`)
- `--up-local` - Run the operator locally instead of as a container in the cluster
- `--no-setup` - Disable test resource creation
- `--image` - Use different image for operator (only supports single deployment with single container)



## Design overview

< Design of how the proposal architecture implementation would look like. >

## User facing usage (if needed)

< If the user needs to interact with this feature, how would it look like from their point of view. >

## Observations and open questions

< Any open questions that need solving or implementation details should go here. These can be removed at the end of the proposal if they are resolved. >
