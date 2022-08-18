// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package utils_test

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gardener/terraformer/pkg/utils"
)

var _ = Describe("Multierror", func() {
	var (
		allErrs    *multierror.Error
		err1, err2 error
	)

	BeforeEach(func() {
		err1 = fmt.Errorf("error 1")
		err2 = fmt.Errorf("error 2")
	})

	Describe("#NewErrorFormatFuncWithPrefix", func() {
		BeforeEach(func() {
			allErrs = &multierror.Error{
				ErrorFormat: utils.NewErrorFormatFuncWithPrefix("prefix"),
			}
		})

		It("should format a multierror correctly if it contains 1 error", func() {
			allErrs.Errors = []error{err1}
			Expect(allErrs.Error()).To(Equal("prefix: 1 error occurred: error 1"))
		})
		It("should format a multierror correctly if it contains multiple errors", func() {
			allErrs.Errors = []error{err1, err2}
			Expect(allErrs.Error()).To(Equal("prefix: 2 errors occurred: [error 1, error 2]"))
		})
	})
})
