/*
Copyright 2019-2021 Google LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

var negNameTemplate = "{namespace}-{name}-{port}-{hash}"

func TestNegNameGeneration(t *testing.T) {

	negName := generateNegName("ns", "name", "8080", negNameTemplate)
	expectedNegName := "ns-name-8080-" + hash("ns-name-8080")
	if negName != expectedNegName {
		t.Errorf("negName = %q, expect %q", negName, expectedNegName)
	}
}

func TestLongNegNameGeneration(t *testing.T) {

	negName := generateNegName("longlonglonglonglonglonglongnamespace", "longlonglongname", "8080", negNameTemplate)
	expectedNegName := "longlonglonglonglonglonglongnamesp-longlonglongnam-808-" + hash("longlonglonglonglonglonglongnamespace-longlonglongname-8080")
	if negName != expectedNegName {
		t.Errorf("negName = %q, expect %q", negName, expectedNegName)
	}
	if len(negName) != 63 {
		t.Errorf("max neg name length should be 63 but is %q", len(negName))
	}
}

func TestLongNegNamesHaveDifferentHashes(t *testing.T) {

	negName1 := generateNegName("longlonglonglonglonglonglongnamespace1", "longlonglongname", "8080", negNameTemplate)
	negName2 := generateNegName("longlonglonglonglonglonglongnamespace2", "longlonglongname", "8080", negNameTemplate)
	if negName1 == negName2 {
		t.Errorf("negName1 should be different from negName2, but both are %q", negName1)
	}
}

func TestValidNegTemplate(t *testing.T) {
	validTemplates := []string{"{namespace}", "{name}", "{port}", "{hash}", "{name}-{port}", "{namespace}-{name}-{port}-{hash}"}
	for _, template := range validTemplates {
		if !IsValidNEGTemplate(template) {
			t.Errorf("Template %q should be valid but is considered invalid", template)
		}
	}
}

func TestInvalidNegTemplate(t *testing.T) {
	invalidTemplates := []string{"{namespace", "", "-", "{has}", "{name}--{port}", "{namespace};{name};{port};{hash}"}
	for _, template := range invalidTemplates {
		if IsValidNEGTemplate(template) {
			t.Errorf("Template %q should be invalid but is considered valid", template)
		}
	}
}

func TestLongNegGenerationWithoutHash(t *testing.T) {
	negName := generateNegName("longlonglonglonglonglonglongnamespace", "longlonglonglonglonglongname", "8080", "{namespace}-{name}-{port}")
	expectedNegName := "longlonglonglonglonglonglongnames-longlonglonglonglonglongn-808"
	if negName != expectedNegName {
		t.Errorf("negName = %q, expect %q", negName, expectedNegName)
	}
	if len(negName) != 63 {
		t.Errorf("max neg name length should be 63 but is %q", len(negName))
	}
}

func TestLongNegGenerationWithMultipleHashes(t *testing.T) {
	negName := generateNegName("longlonglonglongnamespace", "longlonglongname", "8080", "{hash}-{namespace}-{name}-{port}-{hash}")
	hash := hash("longlonglonglongnamespace-longlonglongname-8080")
	expectedNegName := hash + "-longlonglonglongnamespac-longlonglongname-808-" + hash
	if negName != expectedNegName {
		t.Errorf("negName = %q, expect %q", negName, expectedNegName)
	}
	if len(negName) != 63 {
		t.Errorf("max neg name length should be 63 but is %q", len(negName))
	}
}

func TestNegGenerationWithoutHash(t *testing.T) {
	negName := generateNegName("namespace", "name", "8080", "{name}-{port}")
	expectedNegName := "name-8080"
	if negName != expectedNegName {
		t.Errorf("negName = %q, expect %q", negName, expectedNegName)
	}
}

func hash(stringToHash string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(stringToHash)))[:8]
}
