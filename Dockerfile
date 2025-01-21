# SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

############# golang-base
ARG PROVIDER=all
FROM golang:1.23.5 AS golang-base

############# terraform-bundle
FROM golang-base AS terraform-base

WORKDIR /tmp/terraformer

# overwrite to build provider-specific image
ARG PROVIDER

# copy provider-specifc TF_VERSION
COPY ./build/$PROVIDER/TF_VERSION .

#RUN wget -O- https://apt.releases.hashicorp.com/gpg | gpg --dearmor | sudo tee /usr/share/keyrings/hashicorp-archive-keyring.gpg && \
# echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list && \
# sudo apt update && sudo apt install terraform


RUN export TF_VERSION=$(cat ./TF_VERSION) && mkdir -p /go/src/github.com/hashicorp && \
    git clone --single-branch --depth 1 --branch v${TF_VERSION} https://github.com/hashicorp/terraform.git /go/src/github.com/hashicorp/terraform && \
    cd /go/src/github.com/hashicorp/terraform && \
    CGO_ENABLED=0 go install

# copy provider-specific terraform-bundle.hcl
COPY ./build/$PROVIDER/terraform-bundle.hcl ./main.tf

# fetch providers locally
RUN mkdir tfproviders && terraform providers mirror tfproviders

############# builder
FROM golang-base AS builder

WORKDIR /go/src/github.com/gardener/terraformer
COPY . .

ARG PROVIDER

RUN make install PROVIDER=$PROVIDER

############# terraformer
FROM alpine:3.21.2 AS terraformer

# add additional packages that are required by provider plugins
RUN apk add --update tzdata

WORKDIR /

ENV TF_DEV=true
ENV TF_RELEASE=true

COPY build/tf_cli_config.tfrc /
ENV TF_CLI_CONFIG_FILE="/tf_cli_config.tfrc"
COPY --from=terraform-base /tmp/terraformer/tfproviders/ /terraform-providers/
COPY --from=terraform-base /go/bin/terraform /bin/terraform
COPY --from=builder /go/bin/terraformer /

ENTRYPOINT ["/terraformer"]

############# dev
FROM golang-base AS dev

WORKDIR /go/src/github.com/gardener/terraformer
VOLUME /go/src/github.com/gardener/terraformer

COPY build/tf_cli_config.tfrc ./.terraform.rc
COPY --from=terraform-base /tmp/terraformer/tfproviders/ /terraform-providers/
COPY --from=terraform-base /go/bin/terraform /bin/terraform

COPY vendor vendor
COPY Makefile VERSION go.mod go.sum ./
