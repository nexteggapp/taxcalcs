package golden

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"google.golang.org/protobuf/reflect/protoregistry"

	_ "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	"github.com/nexteggapp/taxcalcs/taxlisp/types"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// The taxlisp vectors: what taxlisp/parser makes of every federal rule file
// and of a set of snippets, and what taxlisp/types makes of the federal rule
// modules (checked as returns/rules/federal's LoadEngine checks them) and of
// a set of well- and ill-typed programs.

// federalRulesDir is where Bazel puts //returns/rules/federal:rules.
const federalRulesDir = "../returns/rules/federal"

// parsedFile is one source parsed with parser.ParseFile. A federal rule
// file's source is not repeated here: it is the file at
// returns/rules/federal/<path>.
type parsedFile struct {
	Path      string `json:"path"`
	Federal   bool   `json:"federal,omitempty"`
	Src       string `json:"src"`
	Module    string `json:"module"`
	Formatted string `json:"formatted"`
	Error     string `json:"error"`
}

// parsedExpr is one source parsed with parser.ParseExpr.
type parsedExpr struct {
	Src       string `json:"src"`
	Expr      string `json:"expr"`
	Formatted string `json:"formatted"`
	Error     string `json:"error"`
}

// formattedFloat is FormatFloat of the float64 with the given bits.
type formattedFloat struct {
	Bits      uint64 `json:"bits"`
	Formatted string `json:"formatted"`
}

// formattedUSD is FormatUSD of a Money with the given minor units.
type formattedUSD struct {
	Minor     int64  `json:"minor"`
	Formatted string `json:"formatted"`
}

type parseVectors struct {
	Files  []parsedFile     `json:"files"`
	Exprs  []parsedExpr     `json:"exprs"`
	Floats []formattedFloat `json:"floats"`
	USD    []formattedUSD   `json:"usd"`
}

// parserSnippets are whole-file sources: the ones taxlisp/parser's tests
// parse, then more.
var parserSnippets = []string{
	`; Top-level comment
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

(= std-deduction #usd"15750.00")

(def (sum-by T) ((f (Fn (T) Money)) (items (Slice T))) Money
  (fold (fn ((acc Money) (item T)) Money (+ acc (f item))) #usd"0.00" items))

(def sample ((in ret/ReturnInputs)) (Union Money Nil)
  (withs (rate 0.0765
          dt #date"2025-04-15"
          xs [1 2 3])
    (if (> (len xs) 0)
        (round-dollar (* std-deduction rate))
        nil)))
`,
	"(unclosed",
	"[unclosed",
	")",
	`#usd"12.345"`,
	`#date"2025-13-01"`,
	`#unknown"abc"`,
	`"unclosed string`,
	"",
	"   \n\t ",
	"; only a comment",
	"; one\n;two\r\n(a ; inner\n b) ; trailing\n",
	"(a (b c",
	"(a ]",
	"[a )",
	"]",
	"(a))",
	`"bad \q escape"`,
	`"ends in a backslash \`,
	`#usd"abc"`,
	`#usd""`,
	`#usd"  "`,
	`#usd"-"`,
	`#usd"1.2.3"`,
	`#usd"99999999999999999999"`,
	`#usd"1.x"`,
	`#usd" -45.5 "`,
	`#usd"+3"`,
	`#usd"1."`,
	`#usd".5"`,
	`#usd"-0.07"`,
	`#date"2025-1"`,
	`#date"0-01-01"`,
	`#date"10000-01-01"`,
	`#date"2025-00-01"`,
	`#date"2025-01-32"`,
	`#date"2025-01-00"`,
	`#date"x-01-01"`,
	`#date" 2025-4-5 "`,
	`#date"+2025-04-15"`,
	`#date`,
	`#usd 5`,
	`#"x"`,
	`#u1"x"`,
	"'",
	"(quote)",
	"'x '(a b) ''y",
	"' ; c\n x",
	"[1 2 [3]] []",
	"1.5 1e6 1e21 0.0001 0.00001 -0.0 123456.7 1234567.0 3.0 +1.5 1.e5 2e-7 1E3",
	"0x1.8p1 0x1ep3 1_0.5 1__0.5 1e400 -1e400 1e-400 0x1p-1074 0x1p1024",
	".5 -.5 1e 1e+ 1.2.3 5. 007.5 1e1_0",
	"0x1.0p-1074 0x1.8p-1074 0x.8p-1073 0x1.fffffffffffff8p0 0x1.00000000000008p0 0x1.00000000000018p0 0x1.ffffffffffffffp1023 0x1.fffffffffffff7ffp1023 0x123456789abcdef01.5p0 0x_1.0p0 0x1._0p0 0X1.0P1 -0x1.0p0 0x1.0p+1_0",
	"1.7976931348623157e308 1.7976931348623159e308 2.4703282292062327e-324 2.4703282292062328e-324 123456789012345678901234567890.5 0.000000000000000000000000000001e30 9007199254740993.0",
	"0 -0 +5 -9223372036854775808 9223372036854775807 9223372036854775808 1_000 0x10",
	":kw : :a/b :12",
	"a/b /a a/ / a/b/c pkg/Name.with.dots - + * <= nil? true false True",
	`"tab\there" "nl\nx" "cr\rx" "q\"x" "bs\\x" "é ü 中文 😀"`,
	"\"raw\x01\x07\x7f\u00a0\u00ad\u2028\ufeff\U0010FFFF\"",
	"(é 中 😀/x)",
	"(def f ((x Int)) Int\n  ; comment before body\n  x)",
	"(a\r\nb)",
	"(a\"s\"b'c)",
}

// parserExprs are sources for parser.ParseExpr.
var parserExprs = []string{
	"fed/Form1040",
	"(/ a b)",
	"",
	"a b",
	"; c\n(x)",
	"#usd\"1\"",
	"(",
}

func parsedFileFor(t *testing.T, path, src string, federal bool) parsedFile {
	t.Helper()
	pf := parsedFile{Path: path, Federal: federal}
	if !federal {
		pf.Src = src
	}
	mf, err := parser.ParseFile(path, src)
	pf.Error = errText(err)
	if err == nil {
		pf.Module = encode(t, mf)
		pf.Formatted = parser.FormatFile(mf)
	}
	return pf
}

// federalRuleNames is the federal rule files in the order LoadEngine reads
// them.
func federalRuleNames(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(federalRulesDir, "*.tlisp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no .tlisp files under %s", federalRulesDir)
	}
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	sort.Strings(names)
	return names
}

func readFederalRule(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(federalRulesDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTaxlispParse(t *testing.T) {
	var v parseVectors
	for _, name := range federalRuleNames(t) {
		v.Files = append(v.Files, parsedFileFor(t, name, readFederalRule(t, name), true))
	}
	for _, src := range parserSnippets {
		v.Files = append(v.Files, parsedFileFor(t, "bad.tlisp", src, false))
	}
	for _, src := range parserExprs {
		pe := parsedExpr{Src: src}
		e, err := parser.ParseExpr(src)
		pe.Error = errText(err)
		if err == nil {
			pe.Expr = encode(t, e)
			pe.Formatted = parser.FormatExpr(e)
		}
		v.Exprs = append(v.Exprs, pe)
	}
	floats := []float64{
		0, math.Copysign(0, -1), 1, -1, 0.1, 0.0765, 0.5, 1.0 / 3, 2.0 / 3, 12.5, 1e-4, 1e-5,
		1.5e-5, 123456, 999999, 1e6, 1234567, 1e20, 1e21, 1e22, 1e100, 1e-100,
		math.MaxFloat64, math.SmallestNonzeroFloat64, 2.2250738585072014e-308,
		1 << 53, 1<<53 + 2, 9007199254740993, 0.1 + 0.2, 100, 1e5, 123456.789,
		5e-324, 4.9e-324, 1.7976931348623157e308, 0.000123, 0.00001234, 3.14159,
		math.Inf(1), math.Inf(-1), math.NaN(), -2.5e-7, 6.02214076e23,
	}
	for _, f := range floats {
		v.Floats = append(v.Floats, formattedFloat{Bits: math.Float64bits(f), Formatted: parser.FormatFloat(f)})
	}
	for _, minor := range []int64{0, 1, -1, 5, 50, 99, 100, 101, -4550, 1575000, -99, -100,
		math.MaxInt64, math.MinInt64, math.MinInt64 + 1} {
		v.USD = append(v.USD, formattedUSD{Minor: minor, Formatted: parser.FormatUSD(&commonv1.Money{Currency: "USD", Minor: minor})})
	}
	check(t, "taxlisp/parse.json", v)
}

// typeCase is one program checked by a fresh types.Checker over the global
// registry, as taxlisp/types' tests check: the error text, or the types of
// its top-level definitions (every global that is not a builtin of that
// name and type), and, when Expr is set, the type CheckExpr gives it after
// the program.
type typeCase struct {
	Name     string            `json:"name"`
	Src      string            `json:"src"`
	Expr     string            `json:"expr,omitempty"`
	Error    string            `json:"error"`
	Globals  map[string]string `json:"globals,omitempty"`
	ExprType string            `json:"expr_type,omitempty"`
}

type typeVectors struct {
	// Federal is the federal rule modules, checked as LoadEngine does.
	Federal typeCase   `json:"federal"`
	Cases   []typeCase `json:"cases"`
}

// typeCases are the programs taxlisp/types' tests check, then more.
var typeCases = []typeCase{
	{Name: "valid program", Src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

(deftype (Option T) (Union T Nil))

(defstruct Bracket ()
  (floor Money)
  (rate Float))

(defstruct (Chosen T) (T)
  (value T)
  (label String))

(= std-deduction-2025 (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_SINGLE #usd"15750.00"
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY #usd"31500.00"))

(def (unwrap-or T) ((opt (Option T)) (fallback T)) T
  (if (nil? opt)
      fallback
      opt))

(def (sum-by T) ((f (Fn (T) Money)) (items (Slice T))) Money
  (fold (fn ((acc Money) (item T)) Money (+ acc (f item)))
        #usd"0.00"
        items))

(def describe-tin ((tin ret/TaxpayerID)) String
  (typecase (v (. tin 'value))
    String v
    ret/Withheld "withheld"
    Nil "unset"))

(def total-w2-wages ((docs ret/Documents)) Money
  (round-dollar
    (sum-by (fn ((w2 ret/W2Document)) Money
              (. w2 'form.wages_tips_other_compensation))
            (. docs 'w2s))))

(def build-1040 ((in ret/ReturnInputs)) fed/Form1040
  (withs (wages (total-w2-wages (. in 'federal.documents))
          std (lookup-or std-deduction-2025 'fed/FILING_STATUS_SINGLE #usd"0.00")
          taxable (clamp-zero (- wages std)))
    (make-proto 'fed/Form1040
      :filing_status 'fed/FILING_STATUS_SINGLE
      :w2_wages wages
      :adjusted_gross_income wages
      :deduction std
      :taxable_income taxable)))
`},
	{Name: "money plus int", Src: `(def bad ((m Money) (n Int)) Money (+ m n))`},
	{Name: "wrong return type", Src: `(def bad ((m Money)) Int m)`},
	{Name: "non-exhaustive typecase", Src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def bad ((tin ret/TaxpayerID)) String
  (typecase (v (. tin 'value))
    String v
    Nil "none"))
`},
	{Name: "unknown protobuf field", Src: `
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(def bad ((w Money)) fed/Form1040
  (make-proto 'fed/Form1040 :non_existent_line w))
`},
	{Name: "wrong protobuf field type", Src: `
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(def bad () fed/Form1040
  (make-proto 'fed/Form1040 :taxable_income 42))
`},
	{Name: "missing struct field", Src: `
(defstruct Pair () (a Money) (b Money))
(def bad () Pair (make-struct Pair :a #usd"1.00"))
`},
	// Further cases, one or more per checker path.
	{Name: "empty module", Src: ``},
	{Name: "unbound symbol", Src: `(def f () Int y)`},
	{Name: "empty list", Src: `()`},
	{Name: "literals", Src: `(= a Int 1) (= b Float 1.5) (= c Money #usd"1") (= d Date #date"2025-01-02") (= e String "s") (= f Bool true) (= g Symbol :kw) (= h Symbol 'x) (= i Nil nil)`},
	{Name: "constant mismatch", Src: `(= a Int "s")`},
	{Name: "constant arity", Src: `(= a 1)`},
	{Name: "constant target", Src: `(= "a" Int 1)`},
	{Name: "def arity", Src: `(def f () Int)`},
	{Name: "def header", Src: `(def 1 () Int 1)`},
	{Name: "def type param", Src: `(def (f 1) () Int 1)`},
	{Name: "def empty header", Src: `(def () () Int 1)`},
	{Name: "def params not list", Src: `(def f x Int 1)`},
	{Name: "def param untyped", Src: `(def f ((x)) Int 1)`},
	{Name: "def param name", Src: `(def f ((1 Int)) Int 1)`},
	{Name: "def only metadata", Src: `(def f () Int :cite "IRC § 1")`},
	{Name: "def metadata", Src: `(def f () Int :cite "IRC § 63" 1)`},
	{Name: "def multiple bodies", Src: `(def f () Int "a" 1)`},
	{Name: "fn", Src: `(= f (Fn (Int) Int) (fn ((x Int)) Int x))`, Expr: `(fn ((x Int) (y Money)) Money y)`},
	{Name: "fn arity", Src: `(= f (Fn (Int) Int) (fn ((x Int)) Int))`},
	{Name: "fn body", Src: `(= f (Fn (Int) Int) (fn ((x Int)) Int "s"))`},
	{Name: "let", Src: `(def f () Int (let x 1 x))`, Expr: `(let x Float 1.5 x)`},
	{Name: "let declared", Src: `(def f () Int (let x Int "s" x))`},
	{Name: "let arity", Src: `(def f () Int (let x))`},
	{Name: "let var", Src: `(def f () Int (let 1 2 3))`},
	{Name: "with", Src: `(def f () Int (with (a 1 b 2) (+ a b)))`},
	{Name: "with sequential", Src: `(def f () Int (with (a 1 b a) b))`},
	{Name: "withs sequential", Src: `(def f () Int (withs (a 1 b a) b))`},
	{Name: "withs odd", Src: `(def f () Int (withs (a 1 b) b))`},
	{Name: "withs arity", Src: `(def f () Int (withs (a 1)))`},
	{Name: "withs var", Src: `(def f () Int (withs (1 1) 2))`},
	{Name: "if union", Src: ``, Expr: `(if true 1 false "a" #usd"1")`},
	{Name: "if arity", Src: `(def f () Int (if true 1))`},
	{Name: "if condition", Src: `(def f () Int (if 1 1 2))`},
	{Name: "if narrowing", Src: `(def f ((x (Option Money))) Money (if (nil? x) #usd"0" x))`},
	{Name: "if not narrowing", Src: `(def f ((x (Option Money))) Money (if (not (nil? x)) x #usd"0"))`},
	{Name: "isa narrowing", Src: `(def f ((x (Union Money Int String))) (Union Int String) (if (isa x Money) 1 x))`},
	{Name: "isa arity", Src: `(def f ((x Int)) Bool (isa x))`},
	{Name: "isa type", Src: `(def f ((x Int)) Bool (isa x Nope))`},
	{Name: "cond", Src: `(def f ((x Int)) String (cond ((> x 1) "a") (:else "b")))`, Expr: `(cond (true 1) (else "x"))`},
	{Name: "cond no else", Src: `(def f ((x Int)) String (cond ((> x 1) "a")))`},
	{Name: "cond empty", Src: `(def f ((x Int)) String (cond))`},
	{Name: "cond else not last", Src: `(def f ((x Int)) String (cond (else "a") (true "b")))`},
	{Name: "cond clause", Src: `(def f ((x Int)) String (cond (true) (else "b")))`},
	{Name: "cond condition", Src: `(def f ((x Int)) String (cond (x "a") (else "b")))`},
	{Name: "typecase else", Src: `(def f ((x (Union Int String))) Int (typecase (v x) Int v :else 0))`},
	{Name: "typecase unreachable", Src: `(def f ((x (Union Int String))) Int (typecase (v x) Int v Money 1 String 2))`},
	{Name: "typecase arity", Src: `(def f ((x Int)) Int (typecase (v x) Int))`},
	{Name: "typecase binding", Src: `(def f ((x Int)) Int (typecase (v) Int v))`},
	{Name: "typecase var", Src: `(def f ((x Int)) Int (typecase (1 x) Int 1))`},
	{Name: "slice", Src: ``, Expr: `[1 "a" 2]`},
	{Name: "empty slice", Src: `(def f () (Slice Int) [])`},
	{Name: "table", Src: ``, Expr: `(table 'a 1 'b 2.5)`},
	{Name: "table arity", Src: ``, Expr: `(table 'a)`},
	{Name: "generic call", Src: `(def (id T) ((x T)) T x)`, Expr: `(id #usd"1")`},
	{Name: "generic slice call", Src: ``, Expr: `(map (fn ((x Int)) Money (int-to-money x)) [1 2])`},
	{Name: "generic conflict", Src: `(def (pick T) ((a T) (b T)) T a) (def f () Int (pick 1 "s"))`},
	{Name: "generic widening", Src: `(def (pick T) ((a T) (b T)) T a)`, Expr: `(pick 1 (if true 1 nil))`},
	{Name: "generic not inferred", Src: ``, Expr: `(empty-slice)`},
	{Name: "inst", Src: ``, Expr: `(inst empty-slice Money)`},
	{Name: "inst generic fn", Src: `(def (sum-by T) ((f (Fn (T) Money)) (items (Slice T))) Money (sum f items))`, Expr: `(inst sum-by Int)`},
	{Name: "inst arity", Src: ``, Expr: `(inst empty-slice Money Int)`},
	{Name: "inst empty", Src: ``, Expr: `(inst)`},
	{Name: "lookup", Src: `(= t (Table Symbol Money) (table 'a #usd"1"))`, Expr: `(lookup t 'a)`},
	{Name: "fold", Src: ``, Expr: `(fold (fn ((acc Int) (x Money)) Int acc) 0 [#usd"1"])`},
	{Name: "generic arg mismatch", Src: ``, Expr: `(len 1)`},
	{Name: "generic fn arg mismatch", Src: ``, Expr: `(map 1 [1])`},
	{Name: "generic table mismatch", Src: ``, Expr: `(lookup [1] 1)`},
	{Name: "call arity", Src: ``, Expr: `(not true false)`},
	{Name: "call arg type", Src: ``, Expr: `(not 1)`},
	{Name: "call non-function", Src: ``, Expr: `(1 2)`},
	{Name: "call non-function symbol", Src: `(= x Int 1)`, Expr: `(x 2)`},
	{Name: "arithmetic", Src: ``, Expr: `(+ 1 2 3)`},
	{Name: "arithmetic float", Src: ``, Expr: `(max 1.5 2.5)`},
	{Name: "arithmetic arity", Src: ``, Expr: `(+ 1)`},
	{Name: "arithmetic on string", Src: ``, Expr: `(- "a" "b")`},
	{Name: "multiply money float", Src: ``, Expr: `(* #usd"1" 0.5)`},
	{Name: "multiply int money", Src: ``, Expr: `(* 2 #usd"1")`},
	{Name: "multiply float int", Src: ``, Expr: `(* 1.5 2)`},
	{Name: "multiply int float", Src: ``, Expr: `(* 2 1.5)`},
	{Name: "multiply ints", Src: ``, Expr: `(* 2 3)`},
	{Name: "multiply moneys", Src: ``, Expr: `(* #usd"1" #usd"2")`},
	{Name: "multiply arity", Src: ``, Expr: `(* 1 2 3)`},
	{Name: "divide moneys", Src: ``, Expr: `(/ #usd"1" #usd"2")`},
	{Name: "divide money int", Src: ``, Expr: `(/ #usd"1" 2)`},
	{Name: "divide floats", Src: ``, Expr: `(/ 1.0 2)`},
	{Name: "divide ints", Src: ``, Expr: `(/ 1 2)`},
	{Name: "divide int money", Src: ``, Expr: `(/ 1 #usd"2")`},
	{Name: "divide arity", Src: ``, Expr: `(/ 1 2 3)`},
	{Name: "comparison", Src: ``, Expr: `(<= #date"2025-01-01" #date"2025-01-02")`},
	{Name: "comparison mismatch", Src: ``, Expr: `(< 1 1.5)`},
	{Name: "comparison strings", Src: ``, Expr: `(> "a" "b")`},
	{Name: "comparison arity", Src: ``, Expr: `(>= 1)`},
	{Name: "is", Src: ``, Expr: `(is 1 "a")`},
	{Name: "is arity", Src: ``, Expr: `(!= 1)`},
	{Name: "and", Src: ``, Expr: `(and true false)`},
	{Name: "and arity", Src: ``, Expr: `(or)`},
	{Name: "and non-bool", Src: ``, Expr: `(and true 1)`},
	{Name: "quote", Src: ``, Expr: `'x`},
	{Name: "quote arity", Src: ``, Expr: `(quote a b)`},
	{Name: "quote enum value", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `'fed/FILING_STATUS_SINGLE`},
	{Name: "quote unknown enum value", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `'fed/NOT_A_VALUE`},
	{Name: "enum value", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `fed/FILING_STATUS_SINGLE`},
	{Name: "enum value unprefixed package", Src: ``, Expr: `taxprotos.us.federal.v1/FILING_STATUS_SINGLE`},
	{Name: "unknown enum value", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `fed/NOT_A_VALUE`},
	{Name: "use-proto arity", Src: `(use-proto 'a)`},
	{Name: "use-proto package", Src: `(use-proto 1 'a)`},
	{Name: "use-proto alias", Src: `(use-proto 'a 1)`},
	{Name: "deftype arity", Src: `(deftype A)`},
	{Name: "deftype header", Src: `(deftype 1 Int)`},
	{Name: "deftype alias", Src: `(deftype Amount Money) (def f ((a Amount)) Money a)`},
	{Name: "deftype generic", Src: `(deftype (Pair T) (Slice T)) (def f ((a (Pair Int))) (Slice Int) a)`},
	{Name: "deftype generic arity", Src: `(deftype (Pair T) (Slice T)) (def f ((a (Pair Int Int))) (Slice Int) a)`},
	{Name: "defstruct arity", Src: `(defstruct A)`},
	{Name: "defstruct field", Src: `(defstruct A (x))`},
	{Name: "defstruct field name", Src: `(defstruct A (1 Int))`},
	{Name: "defstruct field type", Src: `(defstruct A (x Nope))`},
	{Name: "defstruct no params", Src: `(defstruct A (x Int) (y Money))`, Expr: `(make-struct A :x 1 :y #usd"2")`},
	{Name: "defstruct generic", Src: `(defstruct (Box T) (value T))`, Expr: `(make-struct (Box Int) :value 1)`},
	{Name: "generic struct needs args", Src: `(defstruct (Box T) (value T)) (def f ((b Box)) Int 1)`},
	{Name: "generic struct arity", Src: `(defstruct (Box T) (value T)) (def f ((b (Box Int Int))) Int 1)`},
	{Name: "struct field access", Src: `(defstruct (Box T) (value T)) (def f ((b (Box Money))) Money (. b 'value))`},
	{Name: "struct unknown field", Src: `(defstruct (Box T) (value T)) (def f ((b (Box Money))) Money (. b 'nope))`},
	{Name: "make-struct unknown field", Src: `(defstruct A (x Int))`, Expr: `(make-struct A :x 1 :y 2)`},
	{Name: "make-struct field type", Src: `(defstruct A (x Int))`, Expr: `(make-struct A :x "s")`},
	{Name: "make-struct field keyword", Src: `(defstruct A (x Int))`, Expr: `(make-struct A x 1)`},
	{Name: "make-struct target", Src: ``, Expr: `(make-struct Int)`},
	{Name: "make-struct arity", Src: `(defstruct A (x Int))`, Expr: `(make-struct A :x)`},
	{Name: "struct constructor", Src: `(defstruct (Box T) (value T) (n Int))`, Expr: `Box`},
	{Name: "unknown type constructor", Src: `(def f ((x (Nope Int))) Int 1)`},
	{Name: "empty type", Src: `(def f ((x ())) Int 1)`},
	{Name: "literal type", Src: `(def f ((x 1)) Int 1)`},
	{Name: "unknown type", Src: `(def f ((x Nope)) Int 1)`},
	{Name: "unknown qualified type", Src: `(def f ((x nope/Nope)) Int 1)`},
	{Name: "type types", Src: `(def f ((a (Slice Int)) (b (Option Money)) (c (Table String (Slice Float))) (d (Union Int String Int)) (e (Fn (Int Money) Bool)) (g (Union (Union Int Nil) String))) Int 1)`},
	{Name: "Slice arity", Src: `(def f ((a (Slice))) Int 1)`},
	{Name: "Option arity", Src: `(def f ((a (Option Int Int))) Int 1)`},
	{Name: "Table arity", Src: `(def f ((a (Table Int))) Int 1)`},
	{Name: "Union arity", Src: `(def f ((a (Union))) Int 1)`},
	{Name: "Fn arity", Src: `(def f ((a (Fn (Int)))) Int 1)`},
	{Name: "Fn params", Src: `(def f ((a (Fn Int Int))) Int 1)`},
	{Name: "proto types", Src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(def f ((a ret/ReturnInputs) (b fed/FilingStatus) (c taxcalcs.returns.v1/Documents) (d nexteggapp.taxprotos.common.v1/Money)) Int 1)`},
	{Name: "enum value as type", Src: `
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(def f ((a fed/FILING_STATUS_SINGLE)) Int 1)`},
	{Name: "field as type", Src: `
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(def f ((a fed/Form1040.taxable_income)) Int 1)`},
	{Name: "package as type", Src: `
(use-proto 'nexteggapp.taxprotos.us.federal 'fed)
(def f ((a fed/v1)) Int 1)`},
	{Name: "proto field access", Src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def f ((in ret/ReturnInputs)) Int 1)`, Expr: `(fn ((in ret/ReturnInputs)) (Slice ret/W2Document) (. in 'federal.documents.w2s))`},
	{Name: "proto oneof access", Src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def f ((tin ret/TaxpayerID)) Int 1)`, Expr: `(fn ((tin ret/TaxpayerID)) Bool (nil? (. tin 'value)))`},
	{Name: "proto unknown field access", Src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def f ((in ret/ReturnInputs)) Int (. in 'nope))`},
	{Name: "field access on int", Src: `(def f ((x Int)) Int (. x 'nope))`},
	{Name: "field access path", Src: `(def f ((x Int)) Int (. x 1))`},
	{Name: "field access arity", Src: `(def f ((x Int)) Int (. x))`},
	{Name: "date fields", Src: `(def f ((d Date)) Int (+ (. d 'year) (. d "month") (. d :day)))`},
	{Name: "date unknown field", Src: `(def f ((d Date)) Int (. d 'week))`},
	{Name: "has?", Src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def f ((in ret/ReturnInputs)) Bool (has? in 'federal))`},
	{Name: "has? arity", Src: `(def f ((x Int)) Bool (has? x))`},
	{Name: "which-oneof", Src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def f ((tin ret/TaxpayerID)) (Union Symbol Nil) (which-oneof tin 'value))`},
	{Name: "which-oneof unknown", Src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def f ((tin ret/TaxpayerID)) (Union Symbol Nil) (which-oneof tin 'nope))`},
	{Name: "which-oneof target", Src: `(def f ((x Int)) (Union Symbol Nil) (which-oneof x 'value))`},
	{Name: "which-oneof name", Src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def f ((tin ret/TaxpayerID)) (Union Symbol Nil) (which-oneof tin 1))`},
	{Name: "which-oneof arity", Src: `(def f ((x Int)) Bool (which-oneof x))`},
	{Name: "make-proto money", Src: ``, Expr: `(make-proto 'nexteggapp.taxprotos.common.v1/Money :minor 1 :currency "USD")`},
	{Name: "make-proto date", Src: ``, Expr: `(proto "taxprotos.common.v1/Date" :year 2025)`},
	{Name: "make-proto enum", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `(make-proto 'fed/FilingStatus)`},
	{Name: "make-proto target", Src: ``, Expr: `(make-proto 1)`},
	{Name: "make-proto arity", Src: ``, Expr: `(make-proto)`},
	{Name: "make-proto keyword", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `(make-proto 'fed/Form1040 taxable_income 1)`},
	{Name: "make-proto json name", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `(make-proto 'fed/Form1040 :taxableIncome #usd"1" :filing_status nil)`},
	{Name: "make-proto unknown type", Src: ``, Expr: `(make-proto 'nope/Nope)`},
	{Name: "proto-with", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `(fn ((f fed/Form1040)) fed/Form1040 (proto-with f :taxable_income #usd"1"))`},
	{Name: "proto-with field type", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `(fn ((f fed/Form1040)) fed/Form1040 (proto-with f :taxable_income 1))`},
	{Name: "proto-with unknown field", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `(fn ((f fed/Form1040)) fed/Form1040 (proto-with f :nope 1))`},
	{Name: "proto-with keyword", Src: `(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)`, Expr: `(fn ((f fed/Form1040)) fed/Form1040 (proto-with f nope 1))`},
	{Name: "proto-with target", Src: ``, Expr: `(proto-with 1 :a 1)`},
	{Name: "proto-with arity", Src: ``, Expr: `(proto-with 1)`},
	{Name: "builtins", Src: ``, Expr: `(slice nil not nil? empty-slice len empty? elt subslice append map keep fold some all sum-money sum lookup lookup-or round-dollar join-words)`},
	{Name: "builtin types", Src: ``, Expr: `(slice round-cents round-rate4 floor-float ceil-float clamp-zero abs-money int-to-money int-to-float money-to-float float-to-money date-year date-month date-day)`},
	{Name: "top-level expression error", Src: `(+ 1 "a")`},
	{Name: "forward reference", Src: `(def f () Int (g)) (def g () Int 1)`},
	{Name: "union option unify", Src: `(def (unwrap-or T) ((opt (Union T Nil)) (fallback T)) T (if (nil? opt) fallback opt))`, Expr: `(unwrap-or (if true #usd"1" nil) #usd"0")`},
}

func checkTypeCase(t *testing.T, tc typeCase) typeCase {
	t.Helper()
	builtins := types.NewChecker(nil).Globals
	mf, err := parser.ParseFile("test.tlisp", tc.Src)
	if err != nil {
		t.Fatalf("%s: %v", tc.Name, err)
	}
	c := types.NewChecker(nil)
	if err := c.CheckModule(mf); err != nil {
		tc.Error = err.Error()
		return tc
	}
	tc.Globals = definedGlobals(c, builtins)
	if tc.Expr != "" {
		e, err := parser.ParseExpr(tc.Expr)
		if err != nil {
			t.Fatalf("%s: %v", tc.Name, err)
		}
		et, err := c.CheckExpr(e, map[string]types.Type{}, map[string]bool{})
		if err != nil {
			tc.Error = err.Error()
			return tc
		}
		tc.ExprType = et.String()
	}
	return tc
}

// definedGlobals is c's globals that are not builtins of the same type.
func definedGlobals(c *types.Checker, builtins map[string]types.Type) map[string]string {
	out := map[string]string{}
	for name, typ := range c.Globals {
		if b, ok := builtins[name]; ok && b.String() == typ.String() {
			continue
		}
		out[name] = typ.String()
	}
	return out
}

func TestTaxlispTypes(t *testing.T) {
	var v typeVectors

	// The federal rules, as returns/rules/federal.LoadEngine combines and
	// checks them.
	combined := &taxlispv1.ModuleFile{}
	for _, name := range federalRuleNames(t) {
		mod, err := parser.ParseFile(name, readFederalRule(t, name))
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		combined.Expressions = append(combined.Expressions, mod.GetExpressions()...)
	}
	builtins := types.NewChecker(nil).Globals
	checker := types.NewChecker(protoregistry.GlobalFiles)
	v.Federal.Name = "federal"
	if err := checker.CheckModule(combined); err != nil {
		v.Federal.Error = err.Error()
	} else {
		v.Federal.Globals = definedGlobals(checker, builtins)
	}

	for _, tc := range typeCases {
		v.Cases = append(v.Cases, checkTypeCase(t, tc))
	}
	// Builtins' types, by name.
	all := map[string]string{}
	for name, typ := range builtins {
		all[name] = typ.String()
	}
	v.Cases = append(v.Cases, typeCase{Name: "builtins only", Globals: all})
	check(t, "taxlisp/types.json", v)
}
