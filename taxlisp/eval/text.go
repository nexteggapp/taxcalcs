package eval

import (
	"math/big"
	"strconv"
	"strings"

	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
)

// textBuiltins are the builtins that write figures into sentences, for
// estimated tax's explanations, questions and summaries: joining text, an
// integer in decimal, and an amount as a whole number of cents, from which
// the rules write dollars with thousands separators themselves.
var textBuiltins = []BuiltinVal{
	{
		Name: "concat",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			s, ok := args[0].(SliceVal)
			if !ok {
				return nil, errAt(expr, "concat expects Slice, got %T", args[0])
			}
			var b strings.Builder
			for _, e := range s.Elems {
				w, ok := e.(StringVal)
				if !ok {
					return nil, errAt(expr, "concat expects String elements, got %T", e)
				}
				b.WriteString(w.V)
			}
			return StringVal{V: b.String()}, nil
		},
	},
	{
		Name: "int-to-string",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			n, err := intArg("int-to-string", args, 0, expr)
			if err != nil {
				return nil, err
			}
			return StringVal{V: strconv.FormatInt(n, 10)}, nil
		},
	},
	{
		Name: "money-to-cents",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			m, ok := args[0].(MoneyVal)
			if !ok {
				return nil, errAt(expr, "money-to-cents expects Money, got %T", args[0])
			}
			// Halves away from zero, as round-cents rounds.
			cents := new(big.Rat).Mul(roundRatScale(m.R, 100), big.NewRat(100, 1))
			if !cents.Num().IsInt64() {
				return nil, errAt(expr, "money-to-cents: the result is out of range")
			}
			return IntVal{V: cents.Num().Int64()}, nil
		},
	},
}
