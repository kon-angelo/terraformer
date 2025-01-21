# SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

ENSURE_GARDENER_MOD         := $(shell go get github.com/gardener/gardener@$$(go list -m -f "{{.Version}}" github.com/gardener/gardener))
GARDENER_HACK_DIR           := $(shell go list -m -f "{{.Dir}}" github.com/gardener/gardener)/hack
NAME                 := terraformer
IMAGE_REPOSITORY     := europe-docker.pkg.dev/gardener-project/public/gardener/$(NAME)
IMAGE_REPOSITORY_DEV := $(IMAGE_REPOSITORY)/dev
REPO_ROOT            := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
VERSION              := $(shell cat "$(REPO_ROOT)/VERSION")
EFFECTIVE_VERSION    := $(shell $(REPO_ROOT)/hack/get-version.sh)
HACK_DIR                    := $(REPO_ROOT)/hack

PROVIDER := all
IMAGE_REPOSITORY_PROVIDER := $(IMAGE_REPOSITORY)
ifneq ($(PROVIDER),all)
	IMAGE_REPOSITORY_PROVIDER := $(IMAGE_REPOSITORY)-$(PROVIDER)
endif

# default IMAGE_TAG if unset (overwritten in release test)
ifeq ($(IMAGE_TAG),)
	override IMAGE_TAG = $(EFFECTIVE_VERSION)
endif

LD_FLAGS             := "-w -X github.com/gardener/$(NAME)/pkg/version.gitVersion=$(IMAGE_TAG) -X github.com/gardener/$(NAME)/pkg/version.provider=$(PROVIDER)"

REGION                 := eu-west-1
ACCESS_KEY_ID_FILE     := .kube-secrets/aws/access_key_id.secret
SECRET_ACCESS_KEY_FILE := .kube-secrets/aws/secret_access_key.secret

#########################################
# Tools                                 #
#########################################

TOOLS_DIR := $(HACK_DIR)/tools
include $(GARDENER_HACK_DIR)/tools.mk

#########################################
# Rules for local development scenarios #
#########################################

COMMAND       := apply
ZAP_DEVEL     := true
ZAP_LOG_LEVEL := debug

.PHONY: run
run:
	# running `go run ./cmd/terraformer $(COMMAND)`
	go run -ldflags $(LD_FLAGS) \
		./cmd/terraformer $(COMMAND) \
		--zap-devel=$(ZAP_DEVEL) \
		--zap-log-level=$(ZAP_LOG_LEVEL) \
		--configuration-configmap-name=example.infra.tf-config \
		--state-configmap-name=example.infra.tf-state \
		--variables-secret-name=example.infra.tf-vars

.PHONY: start
start: dev-kubeconfig docker-dev-image
	@docker run -it -v $(shell go env GOCACHE):/root/.cache/go-build \
		-v $(REPO_ROOT):/go/src/github.com/gardener/terraformer \
		-e KUBECONFIG=/go/src/github.com/gardener/terraformer/dev/kubeconfig.yaml \
		-e NAMESPACE=${NAMESPACE} \
		--name terraformer-dev --rm \
		$(IMAGE_REPOSITORY_DEV):$(VERSION) \
		make run COMMAND=$(COMMAND) ZAP_DEVEL=$(ZAP_DEVEL) ZAP_LOG_LEVEL=$(ZAP_LOG_LEVEL)

.PHONY: start-dev-container
start-dev-container: dev-kubeconfig docker-dev-image
	# starting dev container
	@docker run -it -v $(shell go env GOCACHE):/root/.cache/go-build \
		-v $(REPO_ROOT):/go/src/github.com/gardener/terraformer \
		-v $(REPO_ROOT)/bin/container:/go/src/github.com/gardener/terraformer/bin \
		-e KUBEBUILDER_ASSETS=/go/src/github.com/gardener/terraformer/bin/kubebuilder/bin \
		-e KUBECONFIG=/go/src/github.com/gardener/terraformer/dev/kubeconfig.yaml \
		-e NAMESPACE=${NAMESPACE} \
		--name terraformer-dev --rm \
		$(IMAGE_REPOSITORY_DEV):$(VERSION) \
		bash

.PHONY: docker-dev-image
docker-dev-image:
	@DOCKER_BUILDKIT=1 docker build -t $(IMAGE_REPOSITORY_DEV):$(VERSION) --rm --target dev \
		--build-arg BUILDKIT_INLINE_CACHE=1 --build-arg PROVIDER=aws .

.PHONY: dev-kubeconfig
dev-kubeconfig:
	@mkdir -p dev
	@kubectl config view --raw | sed -E 's/127.0.0.1|localhost/host.docker.internal/' > dev/kubeconfig.yaml

#################################################################
# Rules related to binary build, Docker image build and release #
#################################################################

.PHONY: install
install:
	@LD_FLAGS=$(LD_FLAGS) bash $(GARDENER_HACK_DIR)/install.sh ./cmd/terraformer...

.PHONY: build
build: docker-images bundle-clean

.PHONY: release
release: build docker-login docker-push-all

.PHONY: docker-images
docker-images:
	@$(MAKE) docker-image PROVIDER=all
	@$(MAKE) docker-image PROVIDER=alicloud
	@$(MAKE) docker-image PROVIDER=aws
	@$(MAKE) docker-image PROVIDER=azure
	@$(MAKE) docker-image PROVIDER=gcp
	@$(MAKE) docker-image PROVIDER=openstack
	@$(MAKE) docker-image PROVIDER=equinixmetal
	@$(MAKE) docker-image PROVIDER=slim

.PHONY: docker-image
docker-image:
	# building docker image with tag $(IMAGE_REPOSITORY_PROVIDER):$(IMAGE_TAG)
	@DOCKER_BUILDKIT=1 docker build -t $(IMAGE_REPOSITORY_PROVIDER):$(IMAGE_TAG) --rm --target terraformer \
		--build-arg BUILDKIT_INLINE_CACHE=1 --build-arg PROVIDER=$(PROVIDER) -- .

.PHONY: docker-login
docker-login:
	@gcloud auth activate-service-account --key-file .kube-secrets/gcr/gcr-readwrite.json

.PHONY: docker-push-all
docker-push-all:
	@$(MAKE) docker-push PROVIDER=all
	@$(MAKE) docker-push PROVIDER=alicloud
	@$(MAKE) docker-push PROVIDER=aws
	@$(MAKE) docker-push PROVIDER=azure
	@$(MAKE) docker-push PROVIDER=gcp
	@$(MAKE) docker-push PROVIDER=openstack
	@$(MAKE) docker-push PROVIDER=equinixmetal
	@$(MAKE) docker-push PROVIDER=slim

.PHONY: docker-push
docker-push:
	@if ! docker images $(IMAGE_REPOSITORY_PROVIDER) | awk '{ print $$2 }' | grep -q -F $(IMAGE_TAG); then echo "$(IMAGE_REPOSITORY_PROVIDER) version $(IMAGE_TAG) is not yet built. Please run 'make docker-images'"; false; fi
	@gcloud docker -- push $(IMAGE_REPOSITORY_PROVIDER):$(IMAGE_TAG)

.PHONY: bundle-clean
bundle-clean:
	@rm -f terraform-provider*
	@rm -f terraform
	@rm -f terraform*.zip
	@rm -rf bin/

#####################################################################
# Rules for verification, formatting, linting, testing and cleaning #
#####################################################################

.PHONY: tidy
tidy:
	@go mod tidy
	@mkdir -p $(REPO_ROOT)/.ci/hack && cp $(GARDENER_HACK_DIR)/.ci/* $(REPO_ROOT)/.ci/hack/ && chmod +xw $(REPO_ROOT)/.ci/hack/*
	@cp $(GARDENER_HACK_DIR)/cherry-pick-pull.sh $(HACK_DIR)/cherry-pick-pull.sh && chmod +xw $(HACK_DIR)/cherry-pick-pull.sh

.PHONY: clean
clean: bundle-clean
	@bash $(GARDENER_HACK_DIR)/clean.sh ./cmd/... ./pkg/... ./test/...

.PHONY: check-generate
check-generate:
	@bash $(GARDENER_HACK_DIR)/check-generate.sh $(REPO_ROOT)

.PHONY: check
check: $(GOIMPORTS) $(GOLANGCI_LINT) $(HELM)
	@REPO_ROOT=$(REPO_ROOT) bash $(GARDENER_HACK_DIR)/check.sh --golangci-lint-config=./.golangci.yaml ./cmd/... ./pkg/... ./test/...

.PHONY: generate
generate: $(MOCKGEN) $(VGOPATH)
	@REPO_ROOT=$(REPO_ROOT) VGOPATH=$(VGOPATH) GARDENER_HACK_DIR=$(GARDENER_HACK_DIR) bash $(GARDENER_HACK_DIR)/generate-sequential.sh ./cmd/... ./pkg/... ./test/...
	$(MAKE) format

.PHONY: format
format: $(GOIMPORTS) $(GOIMPORTSREVISER)
	@bash $(GARDENER_HACK_DIR)/format.sh ./cmd ./pkg ./test

.PHONY: test
test: $(SETUP_ENVTEST)
	@bash $(GARDENER_HACK_DIR)/test-integration.sh ./cmd/... ./pkg/... ./test/e2e/binary/...

.PHONY: sast
sast: $(GOSEC)
	@bash $(GARDENER_HACK_DIR)/sast.sh

.PHONY: sast-report
sast-report: $(GOSEC)
	@bash $(GARDENER_HACK_DIR)/sast.sh --gosec-report true

.PHONY: test-e2e
test-e2e:
	# Executing pod e2e test with terraformer image $(IMAGE_REPOSITORY)-aws:$(IMAGE_TAG)
	# If the image for this tag is not built/pushed yet, you have to do so first.
	# Or you can use a specific image tag by setting the IMAGE_TAG variable
	# like this: `make test-e2e IMAGE_TAG=v2.0.0`
	@go test -timeout=0 -ldflags $(LD_FLAGS) ./test/e2e/pod \
       --v -ginkgo.v -ginkgo.progress \
       --kubeconfig="${KUBECONFIG}" \
       --access-key-id="$(shell cat $(ACCESS_KEY_ID_FILE))" \
       --secret-access-key="$(shell cat $(SECRET_ACCESS_KEY_FILE))" \
       --region="$(REGION)"

.PHONY: test-cov
test-cov: $(SETUP_ENVTEST)
	@bash $(GARDENER_HACK_DIR)/test-cover.sh ./cmd/... ./pkg/... ./test/e2e/binary/...

.PHONY: test-cov-clean
test-cov-clean: $(SETUP_ENVTEST)
	@bash $(GARDENER_HACK_DIR)/test-cover-clean.sh

.PHONY: verify
verify: check format test sast

.PHONY: verify-extended
verify-extended: check-generate check format test-cov test-cov-clean sast-report
