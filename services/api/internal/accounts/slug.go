package accounts

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	minSlugLength = 2
	maxSlugLength = 63
	suffixSize    = 4
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

func ValidSlug(slug string) bool {
	return slugPattern.MatchString(slug)
}

func Slugify(value string) string {
	var builder strings.Builder
	previousDash := false
	for _, symbol := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case symbol < unicode.MaxASCII && (unicode.IsLetter(symbol) || unicode.IsDigit(symbol)):
			builder.WriteRune(symbol)
			previousDash = false
		case previousDash || builder.Len() == 0:
			continue
		default:
			builder.WriteRune('-')
			previousDash = true
		}
		if builder.Len() >= maxSlugLength {
			break
		}
	}

	slug := strings.Trim(builder.String(), "-")
	if len(slug) > maxSlugLength {
		slug = strings.Trim(slug[:maxSlugLength], "-")
	}
	if len(slug) < minSlugLength {
		return ""
	}
	return slug
}

func slugWithSuffix(base string) (string, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}
	trimmed := base
	if len(trimmed)+len(suffix)+1 > maxSlugLength {
		trimmed = strings.Trim(trimmed[:maxSlugLength-len(suffix)-1], "-")
	}
	if len(trimmed) == 0 {
		return suffix, nil
	}
	return trimmed + "-" + suffix, nil
}

const suffixAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

func randomSuffix() (string, error) {
	buf := make([]byte, suffixSize)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate slug suffix: %w", err)
	}
	symbols := make([]byte, suffixSize)
	for i, value := range buf {
		symbols[i] = suffixAlphabet[int(value)%len(suffixAlphabet)]
	}
	return string(symbols), nil
}
