# Copyright 2016 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

DBG_MAKEFILE ?=
ifeq ($(DBG_MAKEFILE),1)
    $(warning ***** starting makefile for goal(s) "$(MAKECMDGOALS)")
    $(warning ***** $(shell date))
else
    # If we're not debugging the Makefile, don't echo recipes.
    MAKEFLAGS += -s
endif


# Old-skool build tools.
#
# Commonly used targets (see each target for more information):
#   all: Build code.
#   test: Run tests.
#   clean: Clean up.

# It's necessary to set this because some docker images don't make sh -> bash.
SHELL := /bin/bash

# We don't need make's built-in rules.
MAKEFLAGS += --no-builtin-rules
.SUFFIXES:

# We want make to yell at us if we use undefined variables.
MAKEFLAGS += --warn-undefined-variables

# Constants used throughout.
OUT_DIR ?= _output
BIN_DIR := $(OUT_DIR)/bin
PRJ_SRC_PATH := k8s.io/kubernetes

# Metadata for driving the build lives here.
META_DIR := .make

#
# Define variables that we use as inputs so we can warn about undefined variables.
#

WHAT ?=
TESTS ?=

GOFLAGS ?=
KUBE_GOFLAGS = $(GOFLAGS)
export KUBE_GOFLAGS GOFLAGS

GOLDFLAGS ?=
KUBE_GOLDFLAGS = $(GOLDFLAGS)
export KUBE_GOLDFLAGS GOLDFLAGS

GOGCFLAGS ?=
KUBE_GOGCFLAGS = $(GOGCFLAGS)
export KUBE_GOGCFLAGS GOGCFLAGS

# Build code.
#
# Args:
#   WHAT: Directory names to build.  If any of these directories has a 'main'
#     package, the build will produce executable files under $(OUT_DIR)/go/bin.
#     If not specified, "everything" will be built.
#   GOFLAGS: Extra flags to pass to 'go' when building.
#   GOLDFLAGS: Extra linking flags passed to 'go' when building.
#   GOGCFLAGS: Additional go compile flags passed to 'go' when building.
#
# Example:
#   make
#   make all
#   make all WHAT=cmd/kubelet GOFLAGS=-v
#   make all GOGCFLAGS="-N -l"
#     Note: Use the -N -l options to disable compiler optimizations an inlining.
#           Using these build options allows you to subsequently use source
#           debugging tools like delve.
.PHONY: all
all: generated_files
	hack/make-rules/build.sh $(WHAT)

# Build ginkgo
#
# Example:
# make ginkgo
ginkgo:
	hack/make-rules/build.sh vendor/github.com/onsi/ginkgo/ginkgo
.PHONY: ginkgo

# Runs all the presubmission verifications.
#
# Args:
#   BRANCH: Branch to be passed to hack/verify-godeps.sh script.
#
# Example:
#   make verify
#   make verify BRANCH=branch_x
.PHONY: verify
verify:
	KUBE_VERIFY_GIT_BRANCH=$(BRANCH) hack/make-rules/verify.sh -v

# Build and run tests.
#
# Args:
#   WHAT: Directory names to test.  All *_test.go files under these
#     directories will be run.  If not specified, "everything" will be tested.
#   TESTS: Same as WHAT.
#   GOFLAGS: Extra flags to pass to 'go' when building.
#   GOLDFLAGS: Extra linking flags to pass to 'go' when building.
#   GOGCFLAGS: Additional go compile flags passed to 'go' when building.
#
# Example:
#   make check
#   make test
#   make check WHAT=pkg/kubelet GOFLAGS=-v
.PHONY: check test
check test: generated_files
	hack/make-rules/test.sh $(WHAT) $(TESTS)

# Build and run integration tests.
#
# Example:
#   make test-integration
.PHONY: test-integration
test-integration: generated_files
	hack/make-rules/test-integration.sh

# Build and run end-to-end tests.
#
# Example:
#   make test-e2e
.PHONY: test-e2e
test-e2e: ginkgo generated_files
	go run hack/e2e.go -v --build --up --test --down

# Build and run node end-to-end tests.
#
# Args:
#  FOCUS: Regexp that matches the tests to be run.  Defaults to "".
#  SKIP: Regexp that matches the tests that needs to be skipped.  Defaults
#    to "".
#  RUN_UNTIL_FAILURE: If true, pass --untilItFails to ginkgo so tests are run
#    repeatedly until they fail.  Defaults to false.
#  REMOTE: If true, run the tests on a remote host instance on GCE.  Defaults
#    to false.
#  IMAGES: For REMOTE=true only.  Comma delimited list of images for creating
#    remote hosts to run tests against.  Defaults to a recent image.
#  LIST_IMAGES: If true, don't run tests.  Just output the list of available
#    images for testing.  Defaults to false.
#  HOSTS: For REMOTE=true only.  Comma delimited list of running gce hosts to
#    run tests against.  Defaults to "".
#  DELETE_INSTANCES: For REMOTE=true only.  Delete any instances created as
#    part of this test run.  Defaults to false.
#  ARTIFACTS: For REMOTE=true only.  Local directory to scp test artifacts into
#    from the remote hosts.  Defaults to ""/tmp/_artifacts".
#  REPORT: For REMOTE=false only.  Local directory to write juntil xml results
#    to.  Defaults to "/tmp/".
#  CLEANUP: For REMOTE=true only.  If false, do not stop processes or delete
#    test files on remote hosts.  Defaults to true.
#  IMAGE_PROJECT: For REMOTE=true only.  Project containing images provided to
#  IMAGES.  Defaults to "kubernetes-node-e2e-images".
#  INSTANCE_PREFIX: For REMOTE=true only.  Instances created from images will
#    have the name "${INSTANCE_PREFIX}-${IMAGE_NAME}".  Defaults to "test".
#
# Example:
#   make test-e2e-node FOCUS=kubelet SKIP=container
#   make test-e2e-node REMOTE=true DELETE_INSTANCES=true
# Build and run tests.
.PHONY: test-e2e-node
test-e2e-node: ginkgo generated_files
	hack/make-rules/test-e2e-node.sh

# Build and run cmdline tests.
#
# Example:
#   make test-cmd
test-cmd:
	@hack/make-rules/test-cmd.sh
.PHONY: test-cmd

# Remove all build artifacts.
#
# Example:
#   make clean
.PHONY: clean
clean: clean_generated clean_meta
	build/make-clean.sh
	rm -rf $(OUT_DIR)
	rm -rf Godeps/_workspace # Just until we are sure it is gone

# Remove make-related metadata files.
#
# Example:
#   make clean_meta
.PHONY: clean_meta
clean_meta:
	rm -rf $(META_DIR)

# Run 'go vet'.
#
# Args:
#   WHAT: Directory names to vet.  All *.go files under these
#     directories will be vetted.  If not specified, "everything" will be
#     vetted.
#
# Example:
#   make vet
#   make vet WHAT=pkg/kubelet
.PHONY: vet
vet:
	hack/make-rules/vet.sh $(WHAT)

# Build a release
#
# Example:
#   make release
.PHONY: release
release: generated_files
	build/release.sh

# Build a release, but skip tests
#
# Example:
#   make release-skip-tests
.PHONY: release-skip-tests quick-release
release-skip-tests quick-release: generated_files
	KUBE_RELEASE_RUN_TESTS=n KUBE_FASTBUILD=true build/release.sh

# Cross-compile for all platforms
#
# Example:
#   make cross
.PHONY: cross
cross:
	hack/make-rules/cross.sh

# Add rules for all directories in cmd/
#
# Example:
#   make kubectl kube-proxy
.PHONY: $(notdir $(abspath $(wildcard cmd/*/)))
$(notdir $(abspath $(wildcard cmd/*/))): generated_files
	hack/make-rules/build.sh cmd/$@

# Include logic for generated files.
include Makefile.generated_files
