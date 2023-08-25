# Copyright 2023-KylinSoft Co.,Ltd.

# kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
# in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
# the scheduling-retries defined in annotations.


.DEFAULT_GOAL := build

# Build Variables
OUT_DIR ?= _build/

# Controller Build Variables
CONTROLLER_BINARY_NAME ?= kse-rescheduler
CONTROLLER_VERSION ?= 1.0
CONTROLLER_VERSION_SUFFIX ?=-beta1
CONTROLLER_BUILD_FLAGS ?= \
	-ldflags="-s -w \
	-X '$(MODULE)pkg/version.GitCommit=$(GIT_COMMIT)' \
	-X '$(MODULE)pkg/version.AppVersion=$(CONTROLLER_VERSION)' \
	-X '$(MODULE)pkg/version.VersionSuffix=$(CONTROLLER_VERSION_SUFFIX)'"

# Scheduler Build Variables
SCHEDULER_BINARY_NAME ?= kube-scheduler
SCHEDULER_VERSION ?= v0.24.13
SCHEDULER_BUILD_FLAGS ?= \
	-ldflags="-w -X 'k8s.io/component-base/version.gitVersion=$(SCHEDULER_VERSION)'"

MODULE = kse/kse-rescheduler/
GIT_COMMIT ?= $(shell git rev-parse HEAD | tr -d "\n")



# Image Variables
UNAME_M ?= $(shell uname -m)
ifeq ($(UNAME_M),x86_64)
	ARCH := amd64
endif

ifeq ($(UNAME_M),aarch64)
	ARCH := arm64
endif

DOCKER ?= $(shell command -v docker 2> /dev/null)
NERDCTL ?= $(shell command -v nerdctl 2> /dev/null)

ifdef DOCKER
    BUILDER ?= docker
else ifdef NERDCTL
    BUILDER ?= nerdctl
else
    $(error Neither docker nor nerdctl found)
endif



IMAGE_URL ?= registry.kse.com

# Controller Image Variables
CONTROLLER_IMAGE_REPOSITORY ?= $(IMAGE_URL)/$(ARCH)/$(CONTROLLER_BINARY_NAME)
CONTROLLER_IMAGE ?= $(CONTROLLER_IMAGE_REPOSITORY):$(CONTROLLER_VERSION)$(CONTROLLER_VERSION_SUFFIX)
CONTROLLER_IMAGE_LABELS ?= \
	--label gitCommit=$(GIT_COMMIT) \
	--label version=$(CONTROLLER_VERSION)$(CONTROLLER_VERSION_SUFFIX)

# Scheduler Image Variables
SCHEDULER_IMAGE_REPOSITORY ?= $(IMAGE_URL)/$(ARCH)/$(SCHEDULER_BINARY_NAME)
SCHEDULER_IMAGE ?= $(SCHEDULER_IMAGE_REPOSITORY):$(SCHEDULER_VERSION)
SCHEDULER_IMAGE_LABELS ?= \
	--label gitCommit=$(GIT_COMMIT) \
	--label version=$(SCHEDULER_VERSION)

clean:
	rm -rfv "$(OUT_DIR)"

test:
	go test -v ./...

tidy:
	@go mod tidy

build: build-controller build-scheduler

build-controller: tidy
	CGO_ENABLED=0 \
	go build \
	-v \
	-o $(OUT_DIR)$(CONTROLLER_BINARY_NAME) \
	$(CONTROLLER_BUILD_FLAGS) \
	cmd/controller/controller.go

build-scheduler: tidy
	CGO_ENABLED=0 \
	go build \
	-v \
	-o $(OUT_DIR)$(SCHEDULER_BINARY_NAME) \
	$(SCHEDULER_BUILD_FLAGS) \
	cmd/scheduler/main.go

image: image-controller image-scheduler

image-controller:
	$(BUILDER) build \
	-t $(CONTROLLER_IMAGE) \
	$(CONTROLLER_IMAGE_LABELS)	\
	-f build/controller/Dockerfile .

image-scheduler:
	$(BUILDER) build \
	-t $(SCHEDULER_IMAGE) \
	$(SCHEDULER_IMAGE_LABELS)	\
	-f build/scheduler/Dockerfile .

# Phony Targets
.PHONY: clean test tidy build image image-controller image-scheduler build-controller build-scheduler
