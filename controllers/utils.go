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
	"strings"
)

var maxNameLength = 63

// Validates the given serviceNameTemplate
// A template should only contain {namespace}, {name}, {port} or {hash} separated by hyphens
func IsValidServiceNameTemplate(serviceNameTemplate string) bool {
	if serviceNameTemplate == "" {
		return false
	}
	for _, tag := range strings.Split(serviceNameTemplate, "-") {
		if tag != "{namespace}" && tag != "{name}" && tag != "{port}" && tag != "{hash}" {
			return false
		}
	}
	return true
}

// The method returns backend service name based on the service namespace, name
// or target port following the given neg name template.
//
// Output name is at most 63 characters.
func generateServiceName(namespace string, name string, portStr string, serviceNameTemplate string) string {
	nameString := strings.Join([]string{namespace, name, portStr}, "-")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(nameString)))[:8]
	serviceTemplateTags := strings.Split(serviceNameTemplate, "-")

	var fieldsToTruncate []string
	for _, field := range serviceTemplateTags {
		switch field {
		case "{namespace}":
			fieldsToTruncate = append(fieldsToTruncate, namespace)
		case "{name}":
			fieldsToTruncate = append(fieldsToTruncate, name)
		case "{port}":
			fieldsToTruncate = append(fieldsToTruncate, portStr)
		}
	}

	// maxNameLength is the max length of service name without hyphens and hash (8). 63 - hashlength - numberOfHyphens
	maxNegLengthWoHash := maxNameLength - strings.Count(serviceNameTemplate, "{hash}")*8 - strings.Count(serviceNameTemplate, "-")
	truncFields := TrimFieldsEvenly(maxNegLengthWoHash, fieldsToTruncate...)

	// form the final neg name string with hash
	var fields []string
	indexTruncFields := 0
	for _, field := range serviceTemplateTags {
		if field == "{hash}" {
			fields = append(fields, hash)
		} else {
			fields = append(fields, truncFields[indexTruncFields])
			indexTruncFields++
		}
	}
	return strings.Join(fields, "-")
}

// This function is mostly copied from:
// https://github.com/kubernetes/ingress-gce/blob/4cb04408a6266b5ea00d9567c6165b9235392972/pkg/utils/namer/utils.go#L27..L62
// TrimFieldsEvenly trims the fields evenly and keeps the total length
// <= max. Truncation is spread in ratio with their original length,
// meaning smaller fields will be truncated less than longer ones.
func TrimFieldsEvenly(max int, fields ...string) []string {
	if max <= 0 {
		return fields
	}
	total := 0
	for _, s := range fields {
		total += len(s)
	}
	if total <= max {
		return fields
	}
	// Distribute truncation evenly among the fields.
	excess := total - max
	remaining := max
	var lengths []int
	for _, s := range fields {
		// Scale truncation to shorten longer fields more than ones that are already short.
		l := len(s) - len(s)*excess/total - 1
		lengths = append(lengths, l)
		remaining -= l
	}
	// Add fractional space that was rounded down.
	for i := 0; i < remaining; i++ {
		lengths[i]++
	}

	var ret []string
	for i, l := range lengths {
		ret = append(ret, fields[i][:l])
	}

	return ret
}
