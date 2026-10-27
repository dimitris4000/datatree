package document

import (
	"math/big"
	"regexp"
	"strings"
)

// Compare decimal digits and their magnitude instead of constructing 10^exponent.
// Even enormous exponents retain exact ordering without enormous allocations.
var normalizedDecimal = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

type queryDecimal struct {
	negative  bool
	digits    string
	magnitude *big.Int
}

func decimalNumber(s string) (queryDecimal, bool) {
	v := queryDecimal{magnitude: new(big.Int)}
	if !normalizedDecimal.MatchString(s) {
		return v, false
	}
	if strings.HasPrefix(s, "-") {
		v.negative = true
		s = s[1:]
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		if _, ok := v.magnitude.SetString(s[i+1:], 10); !ok {
			return v, false
		}
		s = s[:i]
	}
	fraction := 0
	if i := strings.IndexByte(s, '.'); i >= 0 {
		fraction = len(s) - i - 1
		s = s[:i] + s[i+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		v.negative = false
		v.digits = ""
		v.magnitude.SetInt64(0)
		return v, true
	}
	v.magnitude.Add(v.magnitude, big.NewInt(int64(len(s)-fraction)))
	v.digits = strings.TrimRight(s, "0")
	return v, true
}
func queryNumberText(n *Node) (string, bool) {
	value := n.Value
	// YAML supports bases, separators, leading pluses and decimal forms JSON omits.
	if !queryNumber.MatchString(value) {
		value = strings.ReplaceAll(value, "_", "")
		if n.raw.Tag == "!!int" {
			z, ok := new(big.Int).SetString(value, 0)
			if !ok {
				return "", false
			}
			value = z.String()
		} else {
			value = strings.TrimPrefix(value, "+")
			prefix := ""
			if strings.HasPrefix(value, "-") {
				prefix = "-"
				value = value[1:]
			}
			if strings.HasPrefix(value, ".") {
				value = "0" + value
			}
			i := strings.IndexAny(value, "eE")
			mantissa, exp := value, ""
			if i >= 0 {
				mantissa, exp = value[:i], value[i:]
			}
			if strings.HasSuffix(mantissa, ".") {
				mantissa += "0"
			}
			value = prefix + mantissa + exp
		}
	}
	return value, true
}
func compareQueryNumbers(n *Node, literal string) (int, bool) {
	value, ok := queryNumberText(n)
	if !ok {
		return 0, false
	}

	a, ok := decimalNumber(value)
	if !ok {
		return 0, false
	}
	b, ok := decimalNumber(literal)
	if !ok {
		return 0, false
	}
	if a.negative != b.negative {
		if a.negative {
			return -1, true
		}
		return 1, true
	}
	cmp := 0
	if a.digits == "" && b.digits != "" {
		cmp = -1
	} else if a.digits != "" && b.digits == "" {
		cmp = 1
	} else if a.digits != "" {
		cmp = a.magnitude.Cmp(b.magnitude)
		if cmp == 0 {
			length := max(len(a.digits), len(b.digits))
			for i := 0; i < length; i++ {
				x, y := byte('0'), byte('0')
				if i < len(a.digits) {
					x = a.digits[i]
				}
				if i < len(b.digits) {
					y = b.digits[i]
				}
				if x < y {
					cmp = -1
					break
				}
				if x > y {
					cmp = 1
					break
				}
			}
		}
	}
	if a.negative {
		cmp = -cmp
	}
	return cmp, true
}
