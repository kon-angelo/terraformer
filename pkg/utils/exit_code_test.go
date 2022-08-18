// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package utils_test

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gardener/terraformer/pkg/utils"
)

var _ = Describe("WithExitCode", func() {
	var (
		exitCode        int
		err, underlying error
	)

	BeforeEach(func() {
		exitCode = 1
		underlying = errors.New("foo")
		err = utils.WithExitCode{
			Code:       exitCode,
			Underlying: underlying,
		}
	})

	It("should return specified exit code and unwrap underlying", func() {
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("with exit code %d", exitCode))))
		Expect(err).To(MatchError(ContainSubstring(underlying.Error())))

		var withExitCode utils.WithExitCode
		Expect(errors.As(err, &withExitCode)).To(BeTrue())
		Expect(withExitCode.ExitCode()).To(Equal(exitCode))
		Expect(withExitCode.Unwrap()).To(Equal(underlying))
	})
})
