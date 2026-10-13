package core

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxListCursorLength = 256
	maxFilterValueBytes = 255
)

var (
	collectorUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	collectorDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}( \d{2}:\d{2}(:\d{2})?)?$`)
)

// collectorList is the response shape of the collector list endpoints.
type collectorList[T any] struct {
	Data []T `json:"data"`
	Meta struct {
		Total *int `json:"total"`
	} `json:"meta"`
}

// listCursor is the opaque position of a page: the collector offset, bound
// to the filters of the listing that produced it.
type listCursor struct {
	Offset  int    `json:"o"`
	Filters string `json:"f"`
}

// listFingerprint identifies the filters of a listing, so that a cursor is
// refused with other filters.
func listFingerprint(query url.Values) string {
	sum := sha256.Sum256([]byte(query.Encode()))
	return hex.EncodeToString(sum[:8])
}

func encodeListCursor(offset int, fingerprint string) string {
	data, _ := json.Marshal(listCursor{Offset: offset, Filters: fingerprint})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeListCursor(cursor string, fingerprint string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	invalid := fmt.Errorf("cursor is invalid: pass the next_cursor of a previous call with the same filters")
	if len(cursor) > maxListCursorLength {
		return 0, invalid
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, invalid
	}
	var decoded listCursor
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Offset < 1 || decoded.Filters != fingerprint {
		return 0, invalid
	}
	return decoded.Offset, nil
}

func pageLimit(limit int, defaultLimit int, maxLimit int) (int, error) {
	if limit == 0 {
		return defaultLimit, nil
	}
	if limit < 1 || limit > maxLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maxLimit)
	}
	return limit, nil
}

// pageQuery is the collector query of one page: its columns, a stable order,
// the filters, and the page bounds.
func pageQuery(props []string, orderby string, filters []string, limit int, offset int) url.Values {
	query := url.Values{}
	query.Set("props", strings.Join(props, ","))
	query.Set("orderby", orderby)
	for _, filter := range filters {
		query.Add("filter", filter)
	}
	query.Set("limit", strconv.Itoa(limit))
	query.Set("offset", strconv.Itoa(offset))
	return query
}

// filterContains matches value anywhere in prop, case-insensitively. The
// value is quoted into a regular expression, so no character of it is read
// as a collector filter operator.
func filterContains(prop string, value string) string {
	return prop + ":~" + regexp.QuoteMeta(value)
}

// filterEquals matches prop equal to value, case-insensitively as the
// collector compares it. The collector takes the value after "eq:" as is.
func filterEquals(prop string, value string) string {
	return prop + ":eq:" + value
}

// filterEqualsOrFamily matches prop equal to value or, when value ends with a
// dot, such as service., starting with it.
func filterEqualsOrFamily(prop string, value string) string {
	if strings.HasSuffix(value, ".") {
		return prop + ":~^" + regexp.QuoteMeta(value)
	}
	return filterEquals(prop, value)
}

// validateFilterValue checks an optional filter or identifier given by the
// caller.
func validateFilterValue(name string, value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxFilterValueBytes || !utf8.ValidString(value) || value != strings.TrimSpace(value) ||
		strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) }) {
		return fmt.Errorf("%s must be at most %d bytes of printable text without surrounding spaces", name, maxFilterValueBytes)
	}
	return nil
}

func validateCollectorUUID(name string, value string) error {
	if value != "" && !collectorUUIDPattern.MatchString(value) {
		return fmt.Errorf("%s must be a UUID", name)
	}
	return nil
}

// validateCollectorDate checks an optional date given by the caller in the
// collector local time.
func validateCollectorDate(name string, value string) error {
	if value != "" && !collectorDatePattern.MatchString(value) {
		return fmt.Errorf("%s must be a collector date: YYYY-MM-DD, YYYY-MM-DD HH:MM or YYYY-MM-DD HH:MM:SS", name)
	}
	return nil
}

// collectorBool reads the T/F flags of the collector.
func collectorBool(value string) bool {
	return value == "T"
}
