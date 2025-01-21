// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

// BeFileWithContents succeeds if actual is an absolute path to a file, that exists and its contents matches the given
// content matcher.
func BeFileWithContents(contentMatcher types.GomegaMatcher) types.GomegaMatcher {
	return &fileContentsMatcher{contentMatcher}
}

// BeEmptyFile succeeds if actual is an absolute path to a file, that exists and is empty.
func BeEmptyFile() types.GomegaMatcher {
	return BeFileWithContents(BeEmpty())
}

type fileContentsMatcher struct {
	expectedContents types.GomegaMatcher
}

func (matcher *fileContentsMatcher) Match(actual interface{}) (success bool, err error) {
	actualFilename, ok := actual.(string)
	if !ok {
		return false, fmt.Errorf("beFileWithContents matcher expects a file path")
	}

	file, err := os.Open(filepath.Clean(actualFilename))
	if err != nil {
		return false, err
	}

	fileContents, err := io.ReadAll(file)
	if err != nil {
		return false, err
	}

	return matcher.expectedContents.Match(string(fileContents))
}

func (matcher *fileContentsMatcher) FailureMessage(actual interface{}) string {
	return matcher.expectedContents.FailureMessage(actual)
}

func (matcher *fileContentsMatcher) NegatedFailureMessage(actual interface{}) string {
	return matcher.expectedContents.NegatedFailureMessage(actual)
}
