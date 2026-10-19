package core

import (
	"regexp"
	"slices"
	"strings"
)

// The masking of the values that may be a secret or an email address, shared
// by the tools returning what collector users entered or wrote: the values of
// requests, and the definition texts of forms and services. It reduces the
// exposure, it does not guarantee it: a secret under a key that names none
// stays in clear, and the tools say so.

// sensitiveNamePattern tells, by whole words, the names of the inputs and
// keys whose value may be a secret or an email address: admin_password,
// apiKey, Authorization or requestor_email, not private_ip or bypass.
var sensitiveNamePattern = regexp.MustCompile(`(?i)(^|[^a-z])(pass(word|wd|phrase)?s?|pwd|secrets?|tokens?|credentials?|private[ _-]?keys?|(api|ssh|secret|access)[ _-]?keys?|authorization|cookies?|e?[ _-]?mails?)([^a-z]|$)`)

// camelCasePattern splits camelCase names into words: apiKey reads api_Key.
var camelCasePattern = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// emailPattern tells a text that holds an email address.
var emailPattern = regexp.MustCompile(`[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}`)

// personName is the name of a user the collector records as the author or
// the actor of something, its email addresses masked: the collector records
// many of them as an email address, which the tools do not give.
func personName(name string) string {
	return emailPattern.ReplaceAllString(strings.TrimSpace(name), maskedRequestValue)
}

func sensitiveName(names ...string) bool {
	for _, name := range names {
		if name != "" && sensitiveNamePattern.MatchString(camelCasePattern.ReplaceAllString(name, "${1}_${2}")) {
			return true
		}
	}
	return false
}

// yamlKeyLinePattern reads a "key: value" line of YAML, a list item or not.
var yamlKeyLinePattern = regexp.MustCompile(`^(\s*(?:-\s+)?)([^:#\n]+?):(\s+)(\S.*)$`)

// maskValue masks, in a JSON value, the values of the sensitive keys of its
// objects and the strings holding an email address, and tells whether it
// masked any.
func maskValue(value any) (any, bool) {
	switch value := value.(type) {
	case map[string]any:
		masked := make(map[string]any, len(value))
		found := false
		for key, item := range value {
			if sensitiveName(key) {
				masked[key], found = maskedRequestValue, true
				continue
			}
			var itemFound bool
			masked[key], itemFound = maskValue(item)
			found = found || itemFound
		}
		return masked, found
	case []any:
		masked := make([]any, len(value))
		found := false
		for i, item := range value {
			var itemFound bool
			masked[i], itemFound = maskValue(item)
			found = found || itemFound
		}
		return masked, found
	case string:
		if emailPattern.MatchString(value) {
			return maskedRequestValue, true
		}
	}
	return value, false
}

// maskFormYAML masks, in the definition text of a form, the values of the
// keys that may name a secret or an email address, block or flow ones
// included, the email addresses, and the masked default values, then cuts
// it.
func maskFormYAML(text string, masked []string) (string, bool) {
	lines := strings.Split(text, "\n")
	// keyIndent is the indentation of the last sensitive key whose value
	// goes on below it, -1 when none: its more indented lines are its value.
	keyIndent := -1
	for i, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if keyIndent >= 0 {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if indent > keyIndent {
				lines[i] = line[:indent] + maskedRequestValue
				continue
			}
			keyIndent = -1
		}
		match := yamlKeyLinePattern.FindStringSubmatch(line)
		if match == nil {
			// A key alone on its line takes the more indented lines below.
			item := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
			if key, ok := strings.CutSuffix(item, ":"); ok && sensitiveName(strings.TrimSpace(key)) {
				keyIndent = indent
			}
			lines[i] = maskFlowYAML(line)
			continue
		}
		key, value := strings.TrimSpace(match[2]), strings.TrimSpace(match[4])
		switch {
		case sensitiveName(key):
			lines[i] = match[1] + match[2] + ":" + match[3] + maskedRequestValue
			// A block scalar, | or >, holds its value on the lines below.
			if strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
				keyIndent = indent
			}
		case slices.Contains(masked, unquoteYAML(value)):
			// A masked default is masked where it is a value, not in other
			// words that happen to hold it.
			lines[i] = match[1] + match[2] + ":" + match[3] + maskedRequestValue
		default:
			lines[i] = maskFlowYAML(line)
		}
	}
	text = strings.Join(lines, "\n")
	text = emailPattern.ReplaceAllString(text, maskedRequestValue)
	return truncateUTF8(text, maxFormYAML)
}

// yamlFlowPairPattern reads a key: value pair of a flow mapping, such as
// {password: x, user: y}.
var yamlFlowPairPattern = regexp.MustCompile(`([{,]\s*)([^{},:\[\]]+?)(\s*:\s*)([^,{}\[\]]+)`)

// maskFlowYAML masks the values of the sensitive keys of the flow mappings
// of a line.
func maskFlowYAML(line string) string {
	if !strings.Contains(line, "{") {
		return line
	}
	return yamlFlowPairPattern.ReplaceAllStringFunc(line, func(pair string) string {
		match := yamlFlowPairPattern.FindStringSubmatch(pair)
		if !sensitiveName(strings.TrimSpace(match[2])) {
			return pair
		}
		return match[1] + match[2] + match[3] + maskedRequestValue
	})
}

// unquoteYAML reads a plain or quoted YAML scalar.
func unquoteYAML(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}

// configLinePattern reads a "key = value" line of an OpenSVC configuration.
// A key may be scoped to a node, such as schedule@node1.
var configLinePattern = regexp.MustCompile(`^(\s*)([^=#;\[\]\s][^=#;\[\]]*?)(\s*=\s*)(.*)$`)

// configReferenceKeys are the keywords whose values name the keys of the
// secret and configuration objects to read, not secrets: they stay in clear.
var configReferenceKeys = []string{"secrets_environment", "configs_environment"}

// maskServiceConfig masks, in the configuration text of a service, the
// values of the keys that may name a secret or an email address, with their
// continuation lines, and the email addresses of the values and comments,
// then cuts it.
func maskServiceConfig(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	masking := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// A value goes on over the indented lines below its key.
		if trimmed != "" && (line[0] == ' ' || line[0] == '\t') {
			if masking {
				lines[i] = line[:len(line)-len(strings.TrimLeft(line, " \t"))] + maskedRequestValue
			} else {
				lines[i] = emailPattern.ReplaceAllString(line, maskedRequestValue)
			}
			continue
		}
		masking = false
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			lines[i] = emailPattern.ReplaceAllString(line, maskedRequestValue)
			continue
		}
		match := configLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		key, _, _ := strings.Cut(strings.TrimSpace(match[2]), "@")
		if sensitiveName(key) && !slices.Contains(configReferenceKeys, strings.ToLower(key)) {
			lines[i] = match[1] + match[2] + match[3] + maskedRequestValue
			masking = true
			continue
		}
		// Only the value is searched for email addresses: the key may hold a
		// node scope, such as schedule@node1.example.com.
		lines[i] = match[1] + match[2] + match[3] + emailPattern.ReplaceAllString(match[4], maskedRequestValue)
	}
	return truncateUTF8(strings.Join(lines, "\n"), maxServiceConfig)
}
