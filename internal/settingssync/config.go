package settingssync

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	sectionRE    = regexp.MustCompile(`^\s*\[([A-Za-z0-9_.-]+)\]\s*(?:#.*)?$`)
	assignmentRE = regexp.MustCompile(`^\s*([A-Za-z0-9_-]+)\s*=\s*(.*?)\s*$`)
	integerRE    = regexp.MustCompile(`^[-+]?[0-9]+$`)
	floatRE      = regexp.MustCompile(`^[-+]?(?:[0-9]+\.[0-9]*|[0-9]*\.[0-9]+)$`)
)

func stripTOMLComment(raw string) string {
	var quote rune
	escaped := false
	for index, character := range raw {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' || character == '\'' {
			if quote == 0 {
				quote = character
			} else if quote == character {
				quote = 0
			}
			continue
		}
		if character == '#' && quote == 0 {
			return strings.TrimSpace(raw[:index])
		}
	}
	return strings.TrimSpace(raw)
}

func parseTOMLScalar(raw, path string) (any, error) {
	value := stripTOMLComment(raw)
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		var result string
		if err := json.Unmarshal([]byte(value), &result); err != nil {
			return nil, fmt.Errorf("%s has an invalid TOML string", path)
		}
		return result, nil
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return value[1 : len(value)-1], nil
	}
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if integerRE.MatchString(value) || floatRE.MatchString(value) {
		number, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return number, nil
		}
	}
	return nil, fmt.Errorf("%s uses unsupported TOML syntax", path)
}

func encodeTOMLScalar(value any) (string, error) {
	switch typed := value.(type) {
	case bool:
		return strconv.FormatBool(typed), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case string:
		encoded, err := json.Marshal(typed)
		return string(encoded), err
	default:
		return "", fmt.Errorf("internal error: cannot encode TOML value")
	}
}

func scanConfig(text string) (map[string]any, []string, int, error) {
	values := make(map[string]any)
	unknown := make(map[string]struct{})
	excluded := 0
	section := []string{}
	byPath := specsByPath(configSpecs)

	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		if match := sectionRE.FindStringSubmatch(line); match != nil {
			section = strings.Split(match[1], ".")
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		match := assignmentRE.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		parts := append(append([]string{}, section...), match[1])
		path := strings.Join(parts, ".")
		spec, allowed := byPath[path]
		if allowed {
			if _, duplicate := values[path]; duplicate {
				return nil, nil, 0, fmt.Errorf("%s is defined more than once", path)
			}
			value, err := parseTOMLScalar(match[2], path)
			if err != nil {
				return nil, nil, 0, err
			}
			if err := validateValue(spec, value); err != nil {
				return nil, nil, 0, err
			}
			values[path] = value
		} else if len(section) > 0 && section[0] == "desktop" {
			if _, known := knownExcludedDesktopPaths[path]; known {
				excluded++
			} else {
				unknown[path] = struct{}{}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("scan config.toml: %w", err)
	}
	return values, sortedSet(unknown), excluded, nil
}

func preferenceEntries(specs []settingSpec, values map[string]any) map[string]Entry {
	result := make(map[string]Entry, len(specs))
	for _, spec := range specs {
		value, present := values[spec.Path]
		result[spec.Path] = Entry{Present: present, Value: value}
	}
	return result
}

func renderConfig(original string, entries map[string]Entry) ([]byte, error) {
	bySection := make(map[string][]settingSpec)
	for _, spec := range configSpecs {
		section, _ := splitPath(spec.Path)
		key := strings.Join(section, ".")
		bySection[key] = append(bySection[key], spec)
	}

	seen := make(map[string]struct{})
	encountered := map[string]struct{}{"": {}}
	currentSection := ""
	var output strings.Builder

	appendMissing := func(section string) error {
		for _, spec := range bySection[section] {
			if _, ok := seen[spec.Path]; ok {
				continue
			}
			entry := entries[spec.Path]
			if entry.Present {
				_, key := splitPath(spec.Path)
				encoded, err := encodeTOMLScalar(entry.Value)
				if err != nil {
					return err
				}
				fmt.Fprintf(&output, "%s = %s\n", key, encoded)
			}
			seen[spec.Path] = struct{}{}
		}
		return nil
	}

	for _, line := range splitLinesAfter(original) {
		plain := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if match := sectionRE.FindStringSubmatch(plain); match != nil {
			if err := appendMissing(currentSection); err != nil {
				return nil, err
			}
			currentSection = match[1]
			encountered[currentSection] = struct{}{}
			output.WriteString(line)
			continue
		}
		match := assignmentRE.FindStringSubmatch(plain)
		if match == nil || strings.HasPrefix(strings.TrimSpace(line), "#") {
			output.WriteString(line)
			continue
		}
		path := match[1]
		if currentSection != "" {
			path = currentSection + "." + match[1]
		}
		if _, allowed := specsByPath(configSpecs)[path]; !allowed {
			output.WriteString(line)
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			return nil, fmt.Errorf("%s is defined more than once", path)
		}
		seen[path] = struct{}{}
		entry := entries[path]
		if entry.Present {
			encoded, err := encodeTOMLScalar(entry.Value)
			if err != nil {
				return nil, err
			}
			newline := "\n"
			if strings.HasSuffix(line, "\r\n") {
				newline = "\r\n"
			}
			fmt.Fprintf(&output, "%s = %s%s", match[1], encoded, newline)
		}
	}
	if err := appendMissing(currentSection); err != nil {
		return nil, err
	}

	sections := make([]string, 0, len(bySection))
	for section := range bySection {
		sections = append(sections, section)
	}
	sort.Strings(sections)
	for _, section := range sections {
		if _, ok := encountered[section]; ok {
			continue
		}
		present := make([]settingSpec, 0)
		for _, spec := range bySection[section] {
			seen[spec.Path] = struct{}{}
			if entries[spec.Path].Present {
				present = append(present, spec)
			}
		}
		if len(present) == 0 {
			continue
		}
		if output.Len() > 0 && !strings.HasSuffix(output.String(), "\n\n") {
			output.WriteByte('\n')
		}
		fmt.Fprintf(&output, "[%s]\n", section)
		for _, spec := range present {
			_, key := splitPath(spec.Path)
			encoded, err := encodeTOMLScalar(entries[spec.Path].Value)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&output, "%s = %s\n", key, encoded)
		}
	}
	if len(seen) != len(configSpecs) {
		return nil, fmt.Errorf("internal error: config renderer missed allowlisted keys")
	}
	return []byte(output.String()), nil
}

func splitLinesAfter(text string) []string {
	if text == "" {
		return nil
	}
	lines := bytes.SplitAfter([]byte(text), []byte("\n"))
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) > 0 {
			result = append(result, string(line))
		}
	}
	return result
}
