package golden

// retEvalSources are the taxlisp modules TestTaxlispEvalGolden evaluates: the
// two modules of taxlisp/eval's tests, each ending in the calls those tests
// make, and short modules covering each special form and builtin, their
// rounding, and their runtime errors. An unchecked module skips the
// typechecker, so that the evaluator's own error for an ill-typed form is
// what is recorded.
var retEvalSources = []struct {
	name      string
	src       string
	unchecked bool
}{
	{name: "SSBenefitsWorksheet", src: retEvalSSAndQDCGT + `
(compute-ss-benefits-worksheet
  'fed/FILING_STATUS_MARRIED_FILING_JOINTLY
  false
  [(make-proto 'ret/FormSSA1099Document
     :form (make-proto 'fed/FormSSA1099 :net_benefits #usd"30000.00"))]
  #usd"35000.00" #usd"0.00" #usd"0.00")
`},
	{name: "SSBenefitsWorksheetMFS", src: retEvalSSAndQDCGT + `
(compute-ss-benefits-worksheet
  'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY
  false
  [(make-proto 'ret/FormSSA1099Document
     :form (make-proto 'fed/FormSSA1099 :net_benefits #usd"18333.33"))
   (make-proto 'ret/FormSSA1099Document
     :form (make-proto 'fed/FormSSA1099 :net_benefits #usd"-1200.55"))]
  #usd"12345.67" #usd"10.01" #usd"2500.00")
`},
	{name: "QDCGTWorksheet", src: retEvalSSAndQDCGT + `
(compute-qdcgt-worksheet-mfj-2025 #usd"150000.00" #usd"10000.00" #usd"20000.00")
`},
	{name: "QDCGTWorksheetHighIncome", src: retEvalSSAndQDCGT + `
(compute-qdcgt-worksheet-mfj-2025 #usd"812345.49" #usd"45000.50" #usd"-3000.00")
`},
	{name: "BracketTax", src: retEvalSSAndQDCGT + `
(compute-bracket-tax #usd"1000000.00" ordinary-brackets-2025-mfj)
`},
	{name: "DescribeTINNumber", src: retEvalUnionsGenerics + `
(describe-tin (make-proto 'ret/TaxpayerID :number "123456789"))
`},
	{name: "DescribeTINWithheld", src: retEvalUnionsGenerics + `
(describe-tin (make-proto 'ret/TaxpayerID :withheld (make-proto 'ret/Withheld)))
`},
	{name: "DescribeTINEmpty", src: retEvalUnionsGenerics + `
(describe-tin (make-proto 'ret/TaxpayerID))
`},
	{name: "FullName", src: retEvalUnionsGenerics + `
(full-name "Ada" "Lovelace")
`},
	{name: "BornBeforeCutoff", src: retEvalUnionsGenerics + `
(born-before-cutoff? #date"1959-12-31" #date"1960-01-02")
`},
	{name: "PatchW2Wages", src: retEvalUnionsGenerics + `
(patch-w2-wages
  (make-proto 'ret/W2Document :id "w2-1"
    :form (make-proto 'fed/FormW2 :wages_tips_other_compensation #usd"100000.00"))
  #usd"125000.00")
`},
	{name: "PatchW2WagesKeepsOriginal", src: retEvalUnionsGenerics + `
(let orig ret/W2Document
  (make-proto 'ret/W2Document :id "w2-1"
    :form (make-proto 'fed/FormW2 :wages_tips_other_compensation #usd"100000.00"))
  (let patched ret/W2Document (patch-w2-wages orig #usd"125000.00")
    (slice (. orig 'form.wages_tips_other_compensation)
           (. patched 'form.wages_tips_other_compensation))))
`},
	{name: "UnwrapOr", src: retEvalUnionsGenerics + `
(slice ((inst unwrap-or Int) nil 7) ((inst unwrap-or Int) 3 7))
`},

	// Literals and printing.
	{name: "Nil", src: `nil`},
	{name: "Empty", src: ``},
	{name: "Bool", src: `true`},
	{name: "Int", src: `-42`},
	{name: "String", src: `"tab\there \"quoted\" back\\slash"`},
	{name: "Money", src: `#usd"-1234.5"`},
	{name: "MoneyZero", src: `#usd"0.00"`},
	{name: "Float", src: `3.0`},
	{name: "FloatFraction", src: `0.0765`},
	{name: "FloatExp", src: `1.2e-3`},
	{name: "FloatBig", src: `(* 1000000.0 1000.0)`},
	{name: "FloatTiny", src: `(/ 1.0 300000.0)`},
	{name: "FloatThird", src: `(/ 1.0 3.0)`},
	{name: "Date", src: `#date"2025-04-15"`},
	{name: "Keyword", src: `:line_1`, unchecked: true},
	{name: "Quote", src: `'foo`},
	{name: "QuoteQualified", src: `'fed/FILING_STATUS_SINGLE`},
	{name: "Slice", src: `[1 2 3]`},
	{name: "SliceMixed", src: `(slice #usd"1.00" #usd"2.50")`},
	{name: "Table", src: `(table "a" 1 "b" 2)`},
	{name: "Struct", src: `
(defstruct Point () (x Int) (y Money))
(Point 3 #usd"4.25")
`},
	{name: "MakeStruct", src: `
(defstruct Point () (x Int) (y Money))
(make-struct Point :y #usd"1.00" :x 2)
`},
	{name: "StructField", src: `
(defstruct Point () (x Int) (y Money))
(. (Point 3 #usd"4.25") 'y)
`},
	{name: "StructCtor", src: `
(defstruct Point () (x Int) (y Money))
Point
`},
	{name: "Closure", src: `(def add1 ((x Int)) Int (+ x 1)) add1`},
	{name: "AnonClosure", src: `(fn ((x Int)) Int (+ x 1))`},
	{name: "Builtin", src: `len`},
	{name: "Constant", src: `(= rate Float 0.25) rate`},
	{name: "DefReturnsClosure", src: `(def f ((x Int)) Int x)`},

	// Arithmetic.
	{name: "AddMoney", src: `(+ #usd"0.10" #usd"0.20" #usd"0.30")`},
	{name: "SubMoney", src: `(- #usd"10.00" #usd"0.01" #usd"20.00")`},
	{name: "NegMoney", src: `(- #usd"5.55")`, unchecked: true},
	{name: "NegInt", src: `(- 9)`, unchecked: true},
	{name: "NegFloat", src: `(- 0.5)`, unchecked: true},
	{name: "MinMoney", src: `(min #usd"5.00" #usd"-1.00" #usd"3.00")`},
	{name: "MaxMoney", src: `(max #usd"5.00" #usd"-1.00" #usd"7.00")`},
	{name: "AddInt", src: `(+ 1 2 3)`},
	{name: "MinMaxInt", src: `(slice (min 4 2 9) (max 4 2 9) (- 10 3 2))`},
	{name: "AddFloat", src: `(+ 0.1 0.2)`},
	{name: "MinMaxFloat", src: `(slice (min 0.4 0.2) (max 0.4 0.2) (- 1.0 0.9))`},
	{name: "MulMoneyFloat", src: `(* #usd"1234.57" 0.0765)`},
	{name: "MulFloatMoney", src: `(* 0.153 #usd"100.01")`},
	{name: "MulMoneyInt", src: `(* #usd"2.50" 3)`},
	{name: "MulIntMoney", src: `(* 3 #usd"2.50")`},
	{name: "MulFloats", src: `(* 0.1 3.0)`},
	{name: "MulFloatInt", src: `(slice (* 0.5 3) (* 3 0.5))`},
	{name: "MulInts", src: `(* 6 7)`},
	{name: "DivMoneyFloat", src: `(/ #usd"100.00" 3.0)`},
	{name: "DivMoneyInt", src: `(/ #usd"100.00" 3)`},
	{name: "DivMoneyMoney", src: `(/ #usd"1.00" #usd"3.00")`},
	{name: "DivFloats", src: `(/ 7.0 2.0)`},
	{name: "DivFloatInt", src: `(/ 7.0 2)`},
	{name: "DivInts", src: `(slice (/ 7 2) (/ -7 2))`},
	{name: "DivMoneyFloatZero", src: `(/ #usd"1.00" 0.0)`},
	{name: "DivMoneyIntZero", src: `(/ #usd"1.00" 0)`},
	{name: "DivMoneyMoneyZero", src: `(/ #usd"1.00" #usd"0.00")`},
	{name: "DivFloatZero", src: `(/ 1.0 0.0)`},
	{name: "DivFloatIntZero", src: `(/ 1.0 0)`},
	{name: "DivIntZero", src: `(/ 1 0)`},
	{name: "AddStrings", src: `(+ "a" "b")`, unchecked: true},
	{name: "MulStrings", src: `(* "a" 2)`, unchecked: true},
	{name: "DivStrings", src: `(/ "a" 2)`, unchecked: true},
	{name: "MoneyThirdCents", src: `(round-cents (/ #usd"100.00" 3))`},

	// Rounding.
	{name: "RoundDollarHalfUp", src: `(round-dollar #usd"2.50")`},
	{name: "RoundDollarHalfNeg", src: `(round-dollar #usd"-2.50")`},
	{name: "RoundDollarDown", src: `(round-dollar #usd"2.49")`},
	{name: "RoundCentsHalf", src: `(round-cents (* #usd"0.01" 0.5))`},
	{name: "RoundCentsHalfNeg", src: `(round-cents (* #usd"-0.01" 0.5))`},
	{name: "PrintRoundsHalfCent", src: `(* #usd"0.05" 0.5)`},
	{name: "PrintRoundsThird", src: `(/ #usd"-2.00" 3)`},
	{name: "RoundRate4", src: `(slice (round-rate4 0.123456) (round-rate4 0.00005) (round-rate4 -0.00015))`},
	{name: "FloorCeil", src: `(slice (floor-float 2.7) (ceil-float 2.1) (floor-float -2.5) (ceil-float -2.5))`},
	{name: "ClampZero", src: `(slice (clamp-zero #usd"-3.00") (clamp-zero #usd"3.33") (clamp-zero #usd"0.00"))`},
	{name: "AbsMoney", src: `(abs-money #usd"-3.21")`},
	{name: "Conversions", src: `(slice (int-to-money 12) (float-to-money 0.1) (float-to-money 1234.5678) (float-to-money 1e21))`},
	{name: "IntToFloat", src: `(int-to-float 7)`},
	{name: "MoneyToFloat", src: `(slice (money-to-float #usd"1234.56") (money-to-float (/ #usd"1.00" 3)))`},
	{name: "FloatToMoneyRepeating", src: `(* (float-to-money (/ 1.0 3.0)) 3)`},
	{name: "DateParts", src: `(slice (date-year #date"2025-12-31") (date-month #date"2025-12-31") (date-day #date"2025-12-31"))`},
	{name: "DateFields", src: `(slice (. #date"2026-02-03" 'year) (. #date"2026-02-03" 'month) (. #date"2026-02-03" 'day))`},
	{name: "DateBadField", src: `(. #date"2026-02-03" 'week)`, unchecked: true},

	// Comparison and equality.
	{name: "CompareMoney", src: `(slice (< #usd"1.00" #usd"2.00") (<= #usd"2.00" #usd"2.00") (> #usd"1.00" #usd"2.00") (>= #usd"1.00" #usd"2.00"))`},
	{name: "CompareInt", src: `(slice (< 1 2) (<= 3 2) (> 3 2) (>= 2 2))`},
	{name: "CompareFloat", src: `(slice (< 0.1 0.2) (>= 0.3 (+ 0.1 0.2)))`},
	{name: "CompareDate", src: `(slice (< #date"2025-01-01" #date"2025-01-02") (> #date"2025-02-01" #date"2025-01-31") (<= #date"2024-12-31" #date"2024-12-31"))`},
	{name: "CompareStrings", src: `(< "a" "b")`, unchecked: true},
	{name: "Is", src: `(slice (is 1 1) (is "a" "b") (is #usd"1.0" #usd"1.00") (is nil nil) (is 0.5 0.5) (is #date"2025-01-01" #date"2025-01-01") (is true false))`},
	{name: "IsSymbols", src: `(slice (is 'a 'a) (is 'fed/A 'fed/A) (is 'fed/A 'B))`},
	{name: "NotEqual", src: `(slice (!= 1 2) (!= "a" "a"))`},
	{name: "IsMixed", src: `(is 1 "1")`, unchecked: true},
	{name: "AndOr", src: `(slice (and true true) (and true false) (or false false) (or false true) (and) (or))`, unchecked: true},
	{name: "AndShortCircuit", src: `(and false (is (elt [1] 5) 1))`},
	{name: "OrShortCircuit", src: `(or true (is (elt [1] 5) 1))`},
	{name: "AndNonBool", src: `(and true 1)`, unchecked: true},
	{name: "OrNonBool", src: `(or false "x")`, unchecked: true},
	{name: "Not", src: `(not false)`},
	{name: "NotNonBool", src: `(not 1)`, unchecked: true},

	// Binding and control forms.
	{name: "Let", src: `(let x 5 (+ x 1))`},
	{name: "LetTyped", src: `(let x Int 5 (* x x))`},
	{name: "With", src: `(with (a 1 b 2) (+ a b))`},
	{name: "WithParallel", src: `(let a 10 (with (a 1 b a) b))`},
	{name: "Withs", src: `(withs (a 1 b (+ a 1) c (* b 10)) c)`},
	{name: "WithsTyped", src: `(withs ((a Int) 1 (b Int) (+ a 1)) b)`, unchecked: true},
	{name: "Do", src: `(do 1 2 3)`, unchecked: true},
	{name: "IfElse", src: `(if false 1 true 2 3)`},
	{name: "IfFallThrough", src: `(if false 1 false 2 3)`},
	{name: "IfNonBool", src: `(if 1 2 3)`, unchecked: true},
	{name: "Cond", src: `(cond (false 1) ((is 1 1) 2) (else 3))`},
	{name: "CondElse", src: `(cond (false 1) (:else 3))`, unchecked: true},
	{name: "CondNoMatch", src: `(cond (false 1))`, unchecked: true},
	{name: "CondNonBool", src: `(cond (1 1) (else 2))`, unchecked: true},
	{name: "Typecase", src: `
(def kind ((x (Union Int String Nil))) String
  (typecase (v x)
    Int "int"
    String v
    Nil "nil"))
(slice (kind 1) (kind "s") (kind nil))
`},
	{name: "TypecaseNonExhaustive", src: `(typecase (v 1.5) Int "int" String "s")`, unchecked: true},
	{name: "TypecaseStruct", src: `
(defstruct A () (n Int))
(defstruct B () (s String))
(def f ((x (Union A B))) String
  (typecase (v x)
    A "a"
    B (. v 's)))
(slice (f (A 1)) (f (B "bee")))
`},
	{name: "Isa", src: `
(def money? ((x (Union Money Int))) Bool (isa x Money))
(slice (money? #usd"1.00") (money? 1))
`},
	{name: "IsaUnionSlice", src: `(slice (isa [1] (Slice Int)) (isa 1 (Union String Int)) (isa 1.5 (Union String Int)) (isa #date"2025-01-01" Date) (isa true Bool) (isa nil Nil))`, unchecked: true},
	{name: "NilPredicate", src: `(slice (nil? nil) (nil? 0))`},
	{name: "Recursion", src: `
(def fact ((n Int)) Int (if (<= n 1) 1 (* n (fact (- n 1)))))
(fact 20)
`},
	{name: "ClosureCapture", src: `
(def adder ((n Int)) (Fn (Int) Int) (fn ((x Int)) Int (+ x n)))
((adder 5) 10)
`},
	{name: "DefKeywords", src: `
(def f ((x Int)) Int :cite "26 U.S.C. § 1" (+ x 1))
(f 1)
`},
	{name: "DefMultiBody", src: `(def f ((x Int)) Int 1 2 (+ x 3)) (f 1)`, unchecked: true},
	{name: "ArityMismatch", src: `(def f ((x Int)) Int x) (f 1 2)`, unchecked: true},
	{name: "StructArityMismatch", src: `(defstruct P () (x Int)) (P 1 2)`, unchecked: true},
	{name: "CallNonFunction", src: `(1 2)`, unchecked: true},
	{name: "UnboundSymbol", src: `missing-name`, unchecked: true},
	{name: "UnboundQualified", src: `pkg/missing`, unchecked: true},
	{name: "StructMissingField", src: `(defstruct P () (x Int)) (. (P 1) 'y)`, unchecked: true},
	{name: "FieldOnInt", src: `(. 5 'x)`, unchecked: true},

	// Slices.
	{name: "Len", src: `(slice (len [1 2 3]) (len ((inst empty-slice Int))))`},
	{name: "EmptyP", src: `(slice (empty? [1]) (empty? ((inst empty-slice Int))))`},
	{name: "EmptySlice", src: `((inst empty-slice Int))`},
	{name: "InstBuiltin", src: `(inst empty-slice Int)`},
	{name: "Elt", src: `(elt [10 20 30] 2)`},
	{name: "EltOutOfBounds", src: `(elt [10 20 30] 3)`},
	{name: "EltNegative", src: `(elt [10 20 30] -1)`},
	{name: "Subslice", src: `(slice (subslice [1 2 3 4] 1 3) (subslice [1 2 3 4] -5 99) (subslice [1 2 3 4] 3 1))`},
	{name: "Append", src: `(append [1 2] [3])`},
	{name: "Map", src: `(map (fn ((x Int)) Money (int-to-money x)) [1 2 3])`},
	{name: "Keep", src: `(keep (fn ((x Int)) Bool (> x 1)) [1 2 3])`},
	{name: "KeepNone", src: `(keep (fn ((x Int)) Bool false) [1 2 3])`},
	{name: "Fold", src: `(fold (fn ((acc Int) (x Int)) Int (+ (* acc 10) x)) 0 [1 2 3])`},
	{name: "Some", src: `(slice (some (fn ((x Int)) Bool (> x 2)) [1 2 3]) (some (fn ((x Int)) Bool (> x 5)) [1 2 3]))`},
	{name: "All", src: `(slice (all (fn ((x Int)) Bool (> x 0)) [1 2 3]) (all (fn ((x Int)) Bool (> x 1)) [1 2 3]) (all (fn ((x Int)) Bool false) ((inst empty-slice Int))))`},
	{name: "SumMoney", src: `(sum-money [#usd"1.10" #usd"2.20" #usd"-0.30"])`},
	{name: "Sum", src: `(sum (fn ((x Int)) Money (* #usd"0.33" (int-to-float x))) [1 2 3])`},
	{name: "JoinWords", src: `(join-words ["a" "" "b" "c"])`},
	{name: "Lookup", src: `(slice (lookup (table 1 "one" 2 "two") 2) (lookup (table 1 "one") 3))`, unchecked: true},
	{name: "LookupOr", src: `(lookup-or (table 'fed/A #usd"1.00") 'fed/B #usd"9.00")`},
	{name: "LookupSymbolTable", src: `(lookup-or (table 'fed/A #usd"1.00" 'fed/B #usd"2.00") 'fed/B #usd"9.00")`},
	{name: "LenNonSlice", src: `(len 5)`, unchecked: true},
	{name: "EltBadIndex", src: `(elt [1] "x")`, unchecked: true},
	{name: "MapNonSlice", src: `(map not 5)`, unchecked: true},
	{name: "KeepNonBool", src: `(keep (fn ((x Int)) Int x) [1])`, unchecked: true},
	{name: "SumMoneyNonMoney", src: `(sum-money [1])`, unchecked: true},
	{name: "SumNonMoney", src: `(sum (fn ((x Int)) Int x) [1])`, unchecked: true},
	{name: "JoinWordsNonString", src: `(join-words [1])`, unchecked: true},
	{name: "LookupNonTable", src: `(lookup 1 2)`, unchecked: true},
	{name: "RoundDollarNonMoney", src: `(round-dollar 1)`, unchecked: true},
	{name: "DateYearNonDate", src: `(date-year 1)`, unchecked: true},
	{name: "AppendNonSlice", src: `(append [1] 2)`, unchecked: true},
	{name: "SubsliceBadBounds", src: `(subslice [1] "a" 2)`, unchecked: true},

	// Protobuf messages.
	{name: "MakeProto", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/Person :id "p1" :first_name "Ada" :date_of_birth #date"1990-12-10"
  :blind false :citizenship 'ret/CITIZENSHIP_STATUS_US_CITIZEN)
`},
	{name: "ProtoFieldRead", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(let p ret/Person (make-proto 'ret/Person :id "p1" :date_of_birth #date"1990-12-10"
                    :citizenship 'ret/CITIZENSHIP_STATUS_US_CITIZEN)
  (slice (. p 'id) (. p 'first_name)))
`},
	{name: "ProtoDateRead", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(. (make-proto 'ret/Person :date_of_birth #date"1990-12-10") 'date_of_birth.year)
`},
	{name: "ProtoUnsetDate", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(. (make-proto 'ret/Person) 'date_of_birth)
`},
	{name: "ProtoEnumRead", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(. (make-proto 'ret/Person :citizenship 'ret/CITIZENSHIP_STATUS_US_CITIZEN) 'citizenship)
`},
	{name: "ProtoEnumUnset", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(. (make-proto 'ret/Person) 'citizenship)
`},
	{name: "ProtoEnumCompare", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(let p ret/Person (make-proto 'ret/Person :citizenship 'ret/CITIZENSHIP_STATUS_US_CITIZEN)
  (slice (is (. p 'citizenship) 'ret/CITIZENSHIP_STATUS_US_CITIZEN)
         (is (. p 'citizenship) 'ret/CITIZENSHIP_STATUS_RESIDENT_ALIEN)
         (!= (. p 'citizenship) 'ret/CITIZENSHIP_STATUS_US_CITIZEN)))
`},
	{name: "ProtoEnumBadValue", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/Person :citizenship 'ret/NOT_A_VALUE)
`, unchecked: true},
	{name: "ProtoOptionalBool", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(let p ret/Person (make-proto 'ret/Person :blind false)
  (slice (has? p 'blind) (has? p 'id) (has? (make-proto 'ret/Person) 'blind)))
`},
	{name: "ProtoRepeated", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(let docs ret/Documents
  (make-proto 'ret/Documents
    :w2s [(make-proto 'ret/W2Document :id "a"
            :form (make-proto 'fed/FormW2 :wages_tips_other_compensation #usd"10.00"))
          (make-proto 'ret/W2Document :id "b")])
  (map (fn ((d ret/W2Document)) Money (. d 'form.wages_tips_other_compensation)) (. docs 'w2s)))
`},
	{name: "ProtoRepeatedMessage", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/Documents
  :w2s [(make-proto 'ret/W2Document :id "a") (make-proto 'ret/W2Document :id "b")])
`},
	{name: "ProtoMoneyRounds", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(make-proto 'fed/FormW2
  :wages_tips_other_compensation (/ #usd"100.00" 3)
  :federal_tax_withheld (* #usd"0.01" 0.5)
  :social_security_wages #usd"0.00"
  :social_security_tax_withheld (* #usd"-0.01" 0.5))
`},
	{name: "ProtoNilClears", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(proto-with (make-proto 'ret/Person :id "x" :first_name "Ada") :id nil)
`, unchecked: true},
	{name: "ProtoWith", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(let p ret/Person (make-proto 'ret/Person :id "x" :first_name "Ada")
  (proto-with p :first_name "Grace" :last_name "Hopper"))
`},
	{name: "ProtoWithNonMessage", src: `(proto-with 5 :id "x")`, unchecked: true},
	{name: "ProtoUnsetMessageRead", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(. (make-proto 'ret/W2Document) 'form)
`},
	{name: "ProtoUnsetMoneyRead", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(. (make-proto 'ret/W2Document) 'form.wages_tips_other_compensation)
`},
	{name: "ProtoNoField", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/Person :no_such_field 1)
`, unchecked: true},
	{name: "ProtoReadNoField", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(. (make-proto 'ret/Person) 'no_such_field)
`, unchecked: true},
	{name: "ProtoFieldWrongType", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/Person :id 5)
`, unchecked: true},
	{name: "ProtoMoneyWrongType", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(make-proto 'fed/FormW2 :wages_tips_other_compensation 5)
`, unchecked: true},
	{name: "ProtoRepeatedWrongType", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/Documents :w2s 5)
`, unchecked: true},
	{name: "ProtoUnknownType", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/NoSuchMessage)
`, unchecked: true},
	{name: "ProtoEnumIsNotMessage", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/CitizenshipStatus)
`, unchecked: true},
	{name: "ProtoBadTypeName", src: `(make-proto 5)`, unchecked: true},
	{name: "ProtoOneof", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(let t ret/TaxpayerID (make-proto 'ret/TaxpayerID :number "1")
  (slice (which-oneof t 'value) (which-oneof (make-proto 'ret/TaxpayerID) 'value)
         (. t 'number) (. (make-proto 'ret/TaxpayerID) 'number)))
`, unchecked: true},
	{name: "ProtoOneofSwitch", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(proto-with (make-proto 'ret/TaxpayerID :number "1") :withheld (make-proto 'ret/Withheld))
`},
	{name: "ProtoNoOneof", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(which-oneof (make-proto 'ret/Person) 'nope)
`, unchecked: true},
	{name: "ProtoHasNoField", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(has? (make-proto 'ret/Person) 'nope)
`, unchecked: true},
	{name: "ProtoHasNonMessage", src: `(has? 5 'x)`, unchecked: true},
	{name: "ProtoWhichOneofNonMessage", src: `(which-oneof 5 'x)`, unchecked: true},
	{name: "ProtoEquality", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(slice (is (make-proto 'ret/Person :id "a") (make-proto 'ret/Person :id "a"))
       (is (make-proto 'ret/Person :id "a") (make-proto 'ret/Person :id "b"))
       (is (make-proto 'ret/Person :id "") (make-proto 'ret/Person)))
`},
	{name: "ProtoIsa", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def f ((x (Union ret/Person ret/W2Document))) Bool (isa x ret/Person))
(slice (f (make-proto 'ret/Person)) (f (make-proto 'ret/W2Document)))
`},
	{name: "ProtoQuantity", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(let a ret/DepreciableAsset (make-proto 'ret/DepreciableAsset :recovery_period_years 27.5)
  (slice (. a 'recovery_period_years) (. (make-proto 'ret/DepreciableAsset) 'recovery_period_years)))
`},
	{name: "ProtoQuantityMessage", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/DepreciableAsset :id "a" :recovery_period_years 39.0)
`},
	{name: "ProtoQuantityFraction", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/DepreciableAsset :recovery_period_years (/ 1.0 3.0))
`},
	{name: "ProtoInt32", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(let r ret/ReturnInputs (make-proto 'ret/ReturnInputs :tax_year 2026 :schema_version 1)
  (+ (. r 'tax_year) (. r 'schema_version)))
`},
	{name: "ProtoString", src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(make-proto 'ret/ReturnInputs :tax_year 2026)
`},
	{name: "ProtoFullPackageName", src: `
(make-proto 'nexteggapp.taxcalcs.returns.v1/Person :id "full")
`, unchecked: true},
}

const retEvalSSAndQDCGT = `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

; --- Social Security Benefits Worksheet (Form 1040 Lines 6a & 6b) ---

(= ss-tier1-base (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY #usd"32000.00"
    'fed/FILING_STATUS_SINGLE                 #usd"25000.00"
    'fed/FILING_STATUS_HEAD_OF_HOUSEHOLD      #usd"25000.00"
    'fed/FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE #usd"25000.00"
    'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY   #usd"0.00"))

(= ss-tier2-spread (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY #usd"12000.00"
    'fed/FILING_STATUS_SINGLE                 #usd"9000.00"
    'fed/FILING_STATUS_HEAD_OF_HOUSEHOLD      #usd"9000.00"
    'fed/FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE #usd"9000.00"
    'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY   #usd"0.00"))

(def sum-ssa-net-benefits ((docs (Slice ret/FormSSA1099Document))) Money
  (sum (fn ((d ret/FormSSA1099Document)) Money
         (. d 'form.net_benefits))
       docs))

(def compute-ss-benefits-worksheet
  ((status fed/FilingStatus)
   (lived-apart-all-year Bool)
   (ssa-docs (Slice ret/FormSSA1099Document))
   (other-income Money)
   (tax-exempt-interest Money)
   (adjustments-excluding-student-loan Money))
  ret/SSBenefitsWorksheet

  (withs (line1  (round-dollar (clamp-zero (sum-ssa-net-benefits ssa-docs)))
          line2  (round-dollar (* line1 0.50))
          line3  other-income
          line4  tax-exempt-interest
          line5  (+ line2 line3 line4)
          line6  adjustments-excluding-student-loan
          line7  (- line5 line6)
          mfs-zero (and (is status 'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY)
                        (not lived-apart-all-year))
          line8  (if mfs-zero
                     #usd"0.00"
                     (lookup-or ss-tier1-base status #usd"25000.00"))
          line9  (clamp-zero (- line7 line8))
          line10 (if mfs-zero
                     #usd"0.00"
                     (lookup-or ss-tier2-spread status #usd"9000.00"))
          line11 (clamp-zero (- line9 line10))
          line12 (min line9 line10)
          line13 (round-dollar (* line12 0.50))
          line14 (min line2 line13)
          line15 (round-dollar (* line11 0.85))
          line16 (+ line14 line15)
          line17 (round-dollar (* line1 0.85))
          line18 (if (<= line7 line8)
                     #usd"0.00"
                     (min line16 line17)))
    (make-proto 'ret/SSBenefitsWorksheet
      :line_1_total_benefits line1
      :line_2_half_of_benefits line2
      :line_3_other_income line3
      :line_4_tax_exempt_interest line4
      :line_5_gross_provisional_income line5
      :line_6_adjustments line6
      :line_7_provisional_income line7
      :line_8_base_amount line8
      :line_9_excess_over_base line9
      :line_10_tier_1_width line10
      :line_11_excess_over_adjusted_base line11
      :line_12_tier_1_amount line12
      :line_13_half_of_tier_1 line13
      :line_14_taxable_from_tier_1 line14
      :line_15_eighty_five_percent_of_tier_2 line15
      :line_16_combined_tiers line16
      :line_17_eighty_five_percent_of_benefits line17
      :line_18_taxable_benefits line18)))

; --- Qualified Dividends and Capital Gain Tax Worksheet (QDCGT) ---

(defstruct Bracket ()
  (floor Money)
  (rate  Float))

(= ordinary-brackets-2025-mfj (Slice Bracket)
  [(Bracket #usd"0.00"      0.10)
   (Bracket #usd"23850.00"  0.12)
   (Bracket #usd"96950.00"  0.22)
   (Bracket #usd"206700.00" 0.24)
   (Bracket #usd"394600.00" 0.32)
   (Bracket #usd"501050.00" 0.35)
   (Bracket #usd"751600.00" 0.37)])

(def compute-bracket-tax ((income Money) (brackets (Slice Bracket))) Money
  (let n Int (len brackets)
    (round-dollar
      (fold (fn ((acc Money) (idx Int)) Money
              (withs (b     (elt brackets idx)
                      floor (. b 'floor)
                      rate  (. b 'rate)
                      ceil  (if (< (+ idx 1) n)
                                (. (elt brackets (+ idx 1)) 'floor)
                                income)
                      slice-amt (clamp-zero (- (min income ceil) floor)))
                (+ acc (* slice-amt rate))))
            #usd"0.00"
            [0 1 2 3 4 5 6]))))

(def compute-qdcgt-worksheet-mfj-2025
  ((taxable-income Money)
   (qualified-dividends Money)
   (net-ltcg-and-cap-gain-dist Money))
  ret/QDCGTWorksheet
  (withs (line1  taxable-income
          line2  qualified-dividends
          line3  (clamp-zero net-ltcg-and-cap-gain-dist)
          line4  (+ line2 line3)
          line5  (clamp-zero (- line1 line4))
          line6  #usd"96700.00"
          line7  (min line1 line6)
          line8  (min line5 line7)
          line9  (- line7 line8)
          line10 (min line1 line4)
          line11 line9
          line12 (- line10 line11)
          line13 #usd"600050.00"
          line14 (min line1 line13)
          line15 (+ line5 line9)
          line16 (clamp-zero (- line14 line15))
          line17 (min line12 line16)
          line18 (round-dollar (* line17 0.15))
          line19 (+ line9 line17)
          line20 (- line10 line19)
          line21 (round-dollar (* line20 0.20))
          line22 (compute-bracket-tax line5 ordinary-brackets-2025-mfj)
          line23 (+ line18 line21 line22)
          line24 (compute-bracket-tax line1 ordinary-brackets-2025-mfj)
          line25 (min line23 line24))
    (make-proto 'ret/QDCGTWorksheet
      :line_1_taxable_income line1
      :line_2_qualified_dividends line2
      :line_3_net_capital_gain line3
      :line_4_total_preferential_income line4
      :line_5_ordinary_taxable_income line5
      :line_6_zero_rate_threshold line6
      :line_7_smaller_of_taxable_or_zero_threshold line7
      :line_8_ordinary_in_zero_band line8
      :line_9_taxed_at_zero_percent line9
      :line_10_smaller_of_taxable_or_preferential line10
      :line_11_zero_rate_amount line11
      :line_12_remaining_preferential line12
      :line_13_fifteen_rate_threshold line13
      :line_14_smaller_of_taxable_or_fifteen_threshold line14
      :line_15_ordinary_plus_zero_band line15
      :line_16_fifteen_band_capacity line16
      :line_17_taxed_at_fifteen_percent line17
      :line_18_tax_at_fifteen_percent line18
      :line_19_zero_and_fifteen_preferential line19
      :line_20_taxed_at_twenty_percent line20
      :line_21_tax_at_twenty_percent line21
      :line_22_tax_on_ordinary_income line22
      :line_23_combined_preferential_and_ordinary_tax line23
      :line_24_regular_tax_on_all_taxable_income line24
      :line_25_tax_on_all_taxable_income line25)))
`

const retEvalUnionsGenerics = `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

(deftype (Option T) (Union T Nil))

(def (unwrap-or T) ((x (Option T)) (fallback T)) T
  (if (nil? x) fallback x))

(def describe-tin ((tin ret/TaxpayerID)) String
  (typecase (v (. tin 'value))
    String v
    ret/Withheld "WITHHELD"
    Nil "NONE"))

(def full-name ((first String) (last String)) String
  (join-words [first "" last]))

(def born-before-cutoff? ((dob Date) (cutoff Date)) Bool
  (< dob cutoff))

(def patch-w2-wages ((w2 ret/W2Document) (new-wages Money)) ret/W2Document
  (let updated-form ret/W2Document
    (proto-with w2
      :form (proto-with (. w2 'form)
              :wages_tips_other_compensation new-wages))
    updated-form))
`
