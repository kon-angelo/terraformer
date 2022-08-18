// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer_test

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	corev1 "k8s.io/api/core/v1"

	"github.com/gardener/terraformer/pkg/terraformer"
)

var _ = Describe("ConfigMapStore", func() {
	var (
		s  terraformer.Store
		cm *corev1.ConfigMap
	)

	BeforeEach(func() {
		cm = &corev1.ConfigMap{Data: map[string]string{}}
		s = &terraformer.ConfigMapStore{cm}
	})

	Describe("#Object", func() {
		It("should return the underlying ConfigMap", func() {
			Expect(s.Object()).To(BeIdenticalTo(cm))
		})
	})

	Describe("#Read", func() {
		It("should return error for non-existing key", func() {
			_, err := s.Read("non-existing")
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
		It("should return correct value", func() {
			cm.Data["foo"] = "bar"

			reader, err := s.Read("foo")
			Expect(err).NotTo(HaveOccurred())
			Eventually(gbytes.BufferReader(reader)).Should(gbytes.Say("^bar$"))
		})
	})

	Describe("#Store", func() {
		It("should store given value", func() {
			Expect(s.Store("foo", bytes.NewBufferString("bar"))).To(Succeed())
			Expect(cm.Data).To(HaveKeyWithValue("foo", "bar"))
		})
		It("should store given value (cm.Data=nil)", func() {
			cm.Data = nil
			Expect(s.Store("foo", bytes.NewBufferString("bar"))).To(Succeed())
			Expect(cm.Data).To(HaveKeyWithValue("foo", "bar"))
		})
	})
})

var _ = Describe("SecretStore", func() {
	var (
		s      terraformer.Store
		secret *corev1.Secret
	)

	BeforeEach(func() {
		secret = &corev1.Secret{Data: map[string][]byte{}}
		s = &terraformer.SecretStore{secret}
	})

	Describe("#Object", func() {
		It("should return the underlying Secret", func() {
			Expect(s.Object()).To(BeIdenticalTo(secret))
		})
	})

	Describe("#Read", func() {
		It("should return error for non-existing key", func() {
			_, err := s.Read("non-existing")
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
		It("should return correct value", func() {
			secret.Data["foo"] = []byte("bar")

			reader, err := s.Read("foo")
			Expect(err).NotTo(HaveOccurred())
			Eventually(gbytes.BufferReader(reader)).Should(gbytes.Say("^bar$"))
		})
	})

	Describe("#Store", func() {
		It("should store given value", func() {
			Expect(s.Store("foo", bytes.NewBufferString("bar"))).To(Succeed())
			Expect(secret.Data).To(HaveKeyWithValue("foo", []byte("bar")))
		})
		It("should store given value (secret.Data=nil)", func() {
			secret.Data = nil
			Expect(s.Store("foo", bytes.NewBufferString("bar"))).To(Succeed())
			Expect(secret.Data).To(HaveKeyWithValue("foo", []byte("bar")))
		})
	})
})
