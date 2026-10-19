package cli

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// sizePattern accepts a whole number of gigabytes or terabytes, as in
// PowerShell's 50GB literal. \d and \s are ASCII-only in Go regexps.
var sizePattern = regexp.MustCompile(`(?i)^(\d+)\s*(GB|TB)$`)

// parseSize parses a size such as "50GB", "1tb" or "50 GB" into bytes. The
// units are binary multiples (GB = 2^30, TB = 2^40), like PowerShell. Any
// other form is a usage error.
func parseSize(s string) (uint64, error) {
	m := sizePattern.FindStringSubmatch(s)
	if m == nil {
		return 0, usageErr(fmt.Errorf("invalid size %q: use a whole number of GB or TB, e.g. 50GB", s))
	}
	n, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, usageErr(fmt.Errorf("invalid size %q: number too large", s))
	}
	if n == 0 {
		return 0, usageErr(fmt.Errorf("invalid size %q: must be greater than zero", s))
	}
	shift := 30
	if strings.EqualFold(m[2], "TB") {
		shift = 40
	}
	if n > math.MaxUint64>>shift {
		return 0, usageErr(fmt.Errorf("invalid size %q: too large", s))
	}
	return n << shift, nil
}
