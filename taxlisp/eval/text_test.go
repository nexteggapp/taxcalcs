package eval_test

import (
	"strings"
	"testing"
)

// textCases are the text builtins' cases, in dateCase's shape. The Rust
// engine's tests (rust/taxlisp/tests/text_test.rs) run the same cases.
var textCases = []dateCase{
	// concat joins with nothing between, and keeps empty strings' places.
	{src: `(concat ["About " "$37" "."])`, want: `"About $37."`},
	{src: `(concat ["a" "" "b"])`, want: `"ab"`},
	{src: `(concat ((inst empty-slice String)))`, want: `""`},
	{src: `(concat ["−$1,200"])`, want: `"−$1,200"`},
	{src: `(concat 1)`, err: `concat expects Slice, got eval.IntVal`, unchecked: true},
	{src: `(concat [1 2])`, err: `concat expects String elements, got eval.IntVal`, unchecked: true},
	{src: `(concat [1 2])`, err: `argument 1 has type (Slice Int), want (Slice String)`},

	// int-to-string is decimal, with a minus sign below zero.
	{src: `(int-to-string 2026)`, want: `"2026"`},
	{src: `(int-to-string 0)`, want: `"0"`},
	{src: `(int-to-string -42)`, want: `"-42"`},
	{src: `(int-to-string -9223372036854775808)`, want: `"-9223372036854775808"`},
	{src: `(int-to-string 1.5)`, err: `int-to-string expects Int, got eval.FloatVal`, unchecked: true},

	// money-to-cents rounds halves away from zero, as round-cents does.
	{src: `(money-to-cents #usd"1234.56")`, want: `123456`},
	{src: `(money-to-cents #usd"-0.01")`, want: `-1`},
	{src: `(money-to-cents (/ #usd"0.01" 2))`, want: `1`},
	{src: `(money-to-cents (/ #usd"-0.01" 2))`, want: `-1`},
	{src: `(money-to-cents (/ #usd"0.01" 3))`, want: `0`},
	{src: `(money-to-cents (* #usd"92233720368547758.07" 1))`, want: `9223372036854775807`},
	{src: `(money-to-cents (* #usd"92233720368547758.07" 2))`, err: `money-to-cents: the result is out of range`},
	{src: `(money-to-cents 1)`, err: `money-to-cents expects Money, got eval.IntVal`, unchecked: true},
}

func TestTextBuiltins(t *testing.T) {
	for _, tc := range textCases {
		got, err := evalDateCase(tc)
		switch {
		case tc.err != "":
			if err == nil || !strings.HasSuffix(err.Error(), tc.err) {
				t.Errorf("%s: got %q, err %v; want error %q", tc.src, got, err, tc.err)
			}
		case err != nil || got != tc.want:
			t.Errorf("%s = %q, err %v; want %q", tc.src, got, err, tc.want)
		}
	}
}
