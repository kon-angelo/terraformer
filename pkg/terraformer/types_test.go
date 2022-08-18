// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap/zapcore"

	"github.com/gardener/terraformer/pkg/terraformer"
)

var _ = Describe("Config", func() {
	Describe("#MarshalLogObject", func() {
		It("should marshal config without an error", func() {
			config := &terraformer.Config{}
			Expect(config.MarshalLogObject(zapcore.NewJSONEncoder(zapcore.EncoderConfig{}))).To(Succeed())
		})
	})
})
