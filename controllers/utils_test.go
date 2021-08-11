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

func TestNegNameGeneration(t *testing.T) {

	negName := generateNegName("ns", "name", "8080")
	expectedNegName := "ns-name-8080-" + hash("ns-name-8080")
	if negName != expectedNegName {
		t.Errorf("negName = %q, expect %q", negName, expectedNegName)
	}
}

func TestLongNegNameGeneration(t *testing.T) {

	negName := generateNegName("longlonglonglonglonglonglongnamespace", "longlonglongname", "8080")
	expectedNegName := "longlonglonglonglonglonglongnamesp-longlonglongnam-808-" + hash("longlonglonglonglonglonglongnamespace-longlonglongname-8080")
	if negName != expectedNegName {
		t.Errorf("negName = %q, expect %q", negName, expectedNegName)
	}
}

func TestLongNegNamesHaveDifferentHashes(t *testing.T) {

	negName1 := generateNegName("longlonglonglonglonglonglongnamespace1", "longlonglongname", "8080")
	negName2 := generateNegName("longlonglonglonglonglonglongnamespace2", "longlonglongname", "8080")
	if negName1 == negName2 {
		t.Errorf("negName1 should be different from negName2, but both are %q", negName1)
	}
}

func hash(stringToHash string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(stringToHash)))
}
