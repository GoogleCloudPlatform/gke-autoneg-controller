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

var serviceNameTemplate = "{namespace}-{name}-{port}-{hash}"

func TestServiceNameGeneration(t *testing.T) {

	serviceName := generateServiceName("ns", "name", "8080", serviceNameTemplate)
	expectedServiceName := "ns-name-8080-" + hash("ns;name;8080")
	if serviceName != expectedServiceName {
		t.Errorf("serviceName = %q, expect %q", serviceName, expectedServiceName)
	}
}

func TestLongServiceNameGeneration(t *testing.T) {

	serviceName := generateServiceName("longlonglonglonglonglonglongnamespace", "longlonglongname", "8080", serviceNameTemplate)
	expectedServiceName := "longlonglonglonglonglonglongnamesp-longlonglongnam-808-" + hash("longlonglonglonglonglonglongnamespace;longlonglongname;8080")
	if serviceName != expectedServiceName {
		t.Errorf("serviceName = %q, expect %q", serviceName, expectedServiceName)
	}
	if len(serviceName) != 63 {
		t.Errorf("max service name length should be 63 but is %q", len(serviceName))
	}
}

func TestLongServiceNamesHaveDifferentHashes(t *testing.T) {

	serviceName1 := generateServiceName("longlonglonglonglonglonglongnamespace1", "longlonglongname", "8080", serviceNameTemplate)
	serviceName2 := generateServiceName("longlonglonglonglonglonglongnamespace2", "longlonglongname", "8080", serviceNameTemplate)
	if serviceName1 == serviceName2 {
		t.Errorf("serviceName1 should be different from serviceName2, but both are %q", serviceName1)
	}
}

func TestNamespaceAndServiceConflictHaveDifferentHashes(t *testing.T) {
	// For a template like {namespace}-{name}-{port} without a hash, both of these would become:
	// name-name-name-8080
	serviceName1 := generateServiceName("name-name", "name", "8080", serviceNameTemplate)
	serviceName2 := generateServiceName("name", "name-name", "8080", serviceNameTemplate)
	if serviceName1 == serviceName2 {
		t.Errorf("serviceName1 should be different from serviceName2, but both are %q", serviceName1)
	}
}

func TestValidServiceTemplate(t *testing.T) {
	validTemplates := []string{"{namespace}", "{name}", "{port}", "{hash}", "{name}-{port}", "{namespace}-{name}-{port}-{hash}"}
	for _, template := range validTemplates {
		if !IsValidServiceNameTemplate(template) {
			t.Errorf("Template %q should be valid but is considered invalid", template)
		}
	}
}

func TestInvalidServiceTemplate(t *testing.T) {
	invalidTemplates := []string{"{namespace", "", "-", "{has}", "{name}--{port}", "{namespace};{name};{port};{hash}"}
	for _, template := range invalidTemplates {
		if IsValidServiceNameTemplate(template) {
			t.Errorf("Template %q should be invalid but is considered valid", template)
		}
	}
}

func TestLongServiceGenerationWithoutHash(t *testing.T) {
	serviceName := generateServiceName("longlonglonglonglonglonglongnamespace", "longlonglonglonglonglongname", "8080", "{namespace}-{name}-{port}")
	expectedServiceName := "longlonglonglonglonglonglongnames-longlonglonglonglonglongn-808"
	if serviceName != expectedServiceName {
		t.Errorf("serviceName = %q, expect %q", serviceName, expectedServiceName)
	}
	if len(serviceName) != 63 {
		t.Errorf("max service name length should be 63 but is %q", len(serviceName))
	}
}

func TestLongServiceGenerationWithMultipleHashes(t *testing.T) {
	serviceName := generateServiceName("longlonglonglongnamespace", "longlonglongname", "8080", "{hash}-{namespace}-{name}-{port}-{hash}")
	hash := hash("longlonglonglongnamespace;longlonglongname;8080")
	expectedServiceName := hash + "-longlonglonglongnamespac-longlonglongname-808-" + hash
	if serviceName != expectedServiceName {
		t.Errorf("serviceName = %q, expect %q", serviceName, expectedServiceName)
	}
	if len(serviceName) != 63 {
		t.Errorf("max service name length should be 63 but is %q", len(serviceName))
	}
}

func TestServiceGenerationWithoutHash(t *testing.T) {
	serviceName := generateServiceName("namespace", "name", "8080", "{name}-{port}")
	expectedServiceName := "name-8080"
	if serviceName != expectedServiceName {
		t.Errorf("serviceName = %q, expect %q", serviceName, expectedServiceName)
	}
}

func hash(stringToHash string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(stringToHash)))[:8]
}
