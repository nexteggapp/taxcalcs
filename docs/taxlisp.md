# The `taxlisp` Programming Language Specification

## Introduction

`taxlisp` is a strongly, statically typed, purely functional Lisp dialect designed for expressing statutory tax law, IRS and state revenue worksheets, and form preparation rules over Protocol Buffer schemas.

`taxlisp` combines four foundational ideas:

1. **Homoiconic Protocol Buffer Representation**: Every `taxlisp` program has two isomorphic representations: human-authored UTF-8 S-expression text (`.tlisp`) and a canonical Protocol Buffer syntax tree (`nexteggapp.taxcalcs.taxlisp.v1.ModuleFile`). Parsing and formatting form a lossless round-trip.
2. **Arc-Inspired Concision without Conses**: `taxlisp` adopts concise naming and binding forms inspired by Paul Graham's Arc (`def`, `fn`, `let`, `with`, `withs`, variadic `if`, `is`, `=`), but replaces linked-list cons cells (`car`/`cdr`) with typed, contiguous immutable slices `(Slice T)`.
3. **Mandatory Static Types, Generics, and Union Types**: Every top-level constant (`=`), function parameter, function return type (`def`, `fn`), and struct field (`defstruct`) requires an explicit static type annotation. The type system supports parametric polymorphism (generics), untagged union types `(Union T1 T2 ...)`, exhaustive `typecase` dispatch, and flow-sensitive occurrence typing (`nil?`, `isa`).
4. **Domain-Safe Exact Money Arithmetic, 64-Bit Floats, & Native Protobuf Types**: Currency amounts (`Money`) are first-class primitive types backed by exact rational arithmetic (`math/big.Rat` at runtime), while tax rates, phaseout ratios, and worksheet multipliers use 64-bit IEEE-754 floating-point numbers (`Float` / `float64`) written as bare floating-point literals (`0.0765`, `0.50`, `1.2e-3`). Calendar dates (`Date`) map directly to `taxprotos.common.v1.Date`. Protobuf messages, enums, repeated fields, and `oneof` unions are natively typed and constructed without code generation.

---

## Notation

The syntax is specified using Extended Backus-Naur Form (EBNF):

```ebnf
Production  = production_name "=" [ Expression ] "." .
Expression  = Term { "|" Term } .
Term        = Factor { Factor } .
Factor      = production_name | token | Group | Option | Repetition .
Group       = "(" Expression ")" .
Option      = "[" Expression "]" .
Repetition  = "{" Expression "}" .
```

Productions are expressions constructed from terms and the following operators, in increasing precedence:

- `|` alternation
- `()` grouping
- `[]` option (0 or 1 times)
- `{}` repetition (0 to $n$ times)

Lowercase production names identify lexical tokens; CamelCase names identify syntactic productions.

---

## Source Code and Wire Representation

### Text Representation

Source code is Unicode text encoded in UTF-8. A `.tlisp` source file consists of zero or more top-level expressions separated by whitespace or comments.

### Protocol Buffer Wire Representation

Every `taxlisp` expression corresponds one-to-one with a `nexteggapp.taxcalcs.taxlisp.v1.Expression` message (`taxlisp/v1/taxlisp.proto`):

```protobuf
message Expression {
  oneof value {
    Atom atom = 1;
    List list = 2;
  }
}

message List {
  repeated Expression expressions = 1;
  SyntaxDetails syntax = 3;
}

message Symbol {
  string package = 1;
  string name = 2;
}

message Atom {
  oneof primitive {
    Symbol symbol = 1;
    string keyword = 2;
    string string_value = 3;
    int64 int_value = 4;
    bool bool_value = 5;
    taxprotos.common.v1.Money money = 6;
    double float_value = 7;
    taxprotos.common.v1.Date date = 8;
  }
  SyntaxDetails syntax = 15;
}
```

The `SyntaxDetails` field records source file name, 1-based line and column spans, and leading `;` comments. Typechecking (`taxlisp/types`) and evaluation (`taxlisp/eval`) are completely invariant to `SyntaxDetails`, using it only to attach source locations to diagnostics.

---

## Lexical Elements

### Whitespace and Comments

Whitespace consists of spaces (`U+0020`), horizontal tabs (`U+0009`), carriage returns (`U+000D`), and newlines (`U+000A`). Whitespace separates tokens and is otherwise ignored.

A comment begins with a semicolon `;` outside a string literal and extends to the end of the line. Consecutive `;` comment lines immediately preceding an expression are attached to that expression's `SyntaxDetails.leading_comments`.

### Tokens

```ebnf
ModuleFile   = { Expression } .
Expression   = Atom | List | QuotedExpr | SliceLiteral .
List         = "(" { Expression } ")" .
QuotedExpr   = "'" Expression .
SliceLiteral = "[" { Expression } "]" .
Atom         = BoolLit | IntLit | FloatLit | StringLit | MoneyLit | DateLit
             | Keyword | Symbol .
```

### Symbols and Package-Qualified Symbols

A symbol is an identifier naming a variable, function, type, struct field, or protobuf descriptor. A symbol may optionally include a package/namespace alias prefix separated by `/`:

```ebnf
Symbol       = [ PackageAlias "/" ] SymbolName .
PackageAlias = IdentChar { IdentChar } .
SymbolName   = IdentChar { IdentChar } .
IdentChar    = letter | digit | "_" | "-" | "+" | "*" | "/" | "<" | ">" | "=" | "?" | "!" | "." .
```

- An unqualified symbol `foo` is encoded as `Symbol{name: "foo"}`.
- A qualified symbol `fed/Form1040` is encoded as `Symbol{package: "fed", name: "Form1040"}`.
- A token beginning with `-`, `+`, or a decimal digit that matches `IntLit` or `FloatLit` is lexed as a numeric literal, not a `Symbol`.

### Keywords

A keyword is an identifier prefixed by a colon `:`:

```ebnf
Keyword = ":" SymbolName .
```

Keywords are used as named field specifiers in `make-proto`, `proto-with`, and `make-struct`, and as metadata tags (such as `:cite`) in `def`. A keyword `:line_1a` is stored in `Atom.keyword` as `"line_1a"` (without the leading colon).

### Literals

1. **Booleans (`BoolLit`)**: The tokens `true` and `false` produce `Atom{bool_value: true}` and `Atom{bool_value: false}`.
2. **Integers (`IntLit`)**: Signed 64-bit decimal integers:
   ```ebnf
   IntLit = [ "+" | "-" ] digit { digit } .
   ```
   Produces `Atom{int_value: n}`.
3. **Floating-Point Numbers (`FloatLit`)**: Bare 64-bit IEEE-754 floating-point numbers (tax rates, phaseout fractions, worksheet multipliers):
   ```ebnf
   FloatLit = [ "+" | "-" ] digit { digit } ( "." digit { digit } [ Exponent ] | Exponent ) .
   Exponent = ( "e" | "E" ) [ "+" | "-" ] digit { digit } .
   ```
   Produces `Atom{float_value: f}` (e.g. `0.0765`, `0.50`, `1e-4`).
4. **Strings (`StringLit`)**: Double-quoted UTF-8 strings supporting standard backslash escapes (`\"`, `\\`, `\n`, `\r`, `\t`):
   ```ebnf
   StringLit = '"' { unicode_char | escape_seq } '"' .
   ```
   Produces `Atom{string_value: s}`.
5. **Money Literals (`MoneyLit`)**: Exact USD decimal currency literals prefixed by `#usd`:
   ```ebnf
   MoneyLit = '#usd"' [ "-" ] digit { digit } [ "." digit digit ] '"' .
   ```
   Produces `Atom{money: Money{currency: "USD", minor: cents}}`, where `minor` is the exact signed count of cents (`$15,750.00` $\to$ `1575000`).
6. **Date Literals (`DateLit`)**: ISO-8601 calendar dates prefixed by `#date`:
   ```ebnf
   DateLit = '#date"' year "-" month "-" day '"' .
   ```
   Produces `Atom{date: Date{year: Y, month: M, day: D}}`.

### Reader Abbreviations

Two reader macros expand deterministically into canonical `List` forms at parse time:

| Surface Syntax | Canonical `List` Expansion |
|---|---|
| `'expr` | `(quote expr)` |
| `[e1 e2 ... en]` | `(slice e1 e2 ... en)` |

---

## Types

Every expression in `taxlisp` has a static type known at compile time. Type annotations are mandatory on all declarations (`=`, `def`, `fn`, `defstruct`).

```ebnf
TypeExpr = PrimitiveType
         | TypeVar
         | StructTypeRef
         | ProtoTypeRef
         | SliceType
         | TableType
         | FnType
         | UnionType .
```

### Primitive Types

| Type Name | Description | Runtime Representation | Protobuf Wire Mapping |
|---|---|---|---|
| `Money` | Exact USD currency amount | `*big.Rat` | `taxprotos.common.v1.Money` |
| `Float` | 64-bit IEEE-754 floating-point number (rates, ratios) | `float64` | `double`, `float`, `taxprotos.common.v1.Quantity` |
| `Int` | 64-bit signed integer (counts, indices, tax years) | `int64` | `int32`, `int64`, `uint32`, `uint64` |
| `Bool` | Boolean truth value | `bool` | `bool` |
| `String` | UTF-8 string | `string` | `string` |
| `Date` | Calendar date (`YYYY-MM-DD`) | `(year, month, day)` | `taxprotos.common.v1.Date` |
| `Nil` | Unit type of the singleton `nil` value | `NilVal` | Unset `oneof` or optional field |

`Money`, `Float`, and `Int` are **distinct, non-interchangeable types**. Adding `Money` to `Int` or multiplying `Money` by `Money` is a compile-time type error.

### Composite Types

- **Slice Type**: `(Slice T)` is an immutable, 0-indexed contiguous sequence of elements of type `T`. (`taxlisp` has no cons cells or dotted pairs.)
- **Table Type**: `(Table K V)` is an immutable finite map from keys of type `K` to values of type `V`.
- **Function Type**: `(Fn (T1 T2 ... Tn) R)` is the type of a first-class function taking parameters `(T1 ... Tn)` and returning `R`.

### Algebraic Struct Types

A `defstruct` declaration introduces a nominal product type with named, typed fields. A generic struct `(defstruct (Chosen T) ...)` is referenced in type positions as `(Chosen ConcreteType)`.

### Protobuf Message and Enum Types

When a protobuf package is imported via `(use-proto 'pkg.full.name 'alias)`:
- Any message `Msg` in that package is a static type denoted `alias/Msg`.
- Any enum `Enum` in that package is a static type denoted `alias/Enum`. Enum values are written as quoted symbols `'alias/ENUM_VALUE_NAME` and checked against the `EnumDescriptor` at compile time.

### Union Types

```ebnf
UnionType = "(" "Union" TypeExpr TypeExpr { TypeExpr } ")" .
```

`(Union T1 T2 ... Tn)` represents an untagged union of member types $T_1, \dots, T_n$. Union types obey the following canonical normalization rules:

1. **Flattening**: Nested unions are flattened: `(Union A (Union B C))` $\equiv$ `(Union A B C)`.
2. **Idempotence (Deduplication)**: Duplicate member types are removed: `(Union A B A)` $\equiv$ `(Union A B)`.
3. **Commutativity**: Member order does not affect type equality: `(Union A B)` $\equiv$ `(Union B A)`.
4. **Singleton Collapse**: A union with a single unique member collapses to that member: `(Union A A)` $\equiv$ `A`.

The standard optional pattern is expressed as a union with `Nil`:

```lisp
(deftype (Option T) (Union T Nil))
```

### Type Identity and Assignability

A value of type $S$ is **assignable** to expected type $T$ ($S \le T$) if and only if:

1. $S$ and $T$ are identical types ($S \equiv T$), or
2. $T$ is a `UnionType` `(Union M1 ... Mk)` and $S$ is assignable to at least one member $M_i$ (**union introduction**), or
3. $S$ is a `UnionType` `(Union S1 ... Sm)` and every member $S_j$ is assignable to $T$ (**union subsetting**).

---

## Generics (Parametric Polymorphism)

`taxlisp` supports type parameters on type aliases (`deftype`), structs (`defstruct`), and functions (`def`).

### Generic Type Aliases

```lisp
(deftype (Option T) (Union T Nil))
(deftype (Result T E) (Union T E))
```

### Generic Structs

```lisp
(defstruct (Chosen T)
  (value T)
  (label String))
```

Constructing `(Chosen #usd"15750.00" "standard")` infers `T = Money` and produces a value of type `(Chosen Money)`. Accessing `(. c 'value)` on a `(Chosen Money)` has static type `Money`.

### Generic Functions and Unification

A generic function lists its type parameters in its header `(def (fname T1 T2 ...) ...)`:

```lisp
(def (unwrap-or T) ((opt (Union T Nil)) (fallback T)) T
  (if (nil? opt)
      fallback
      opt))

(def (sum-by T) ((f (Fn (T) Money)) (items (Slice T))) Money
  (fold (fn ((acc Money) (item T)) Money (+ acc (f item)))
        #usd"0.00"
        items))
```

At a call site `(f arg1 ... argn)`, the typechecker performs Robinson unification between the formal parameter patterns and the argument types to solve for `T1, T2, ...`, then substitutes those bindings into the return type.

When a generic function with no value arguments is called (such as `empty-slice`), or when explicit instantiation is desired, `(inst fn Type1 Type2 ...)` instantiates `fn` with the given concrete type arguments:

```lisp
((inst empty-slice ret/W2Document)) ; -> has type (Slice ret/W2Document)
```

---

## Declarations and Scope

A `ModuleFile` is typechecked in two passes:
- **Pass 1** registers all `use-proto` aliases, `deftype` aliases, `defstruct` definitions (and their constructors), and `def` function signatures. Functions within a module may therefore call one another regardless of declaration order.
- **Pass 2** typechecks all `=` constant initializers and `def` function bodies.

### `use-proto`

Binds a short alias to a Protocol Buffer package namespace:

```lisp
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
```

### `deftype`

Declares a transparent type alias (optionally generic):

```ebnf
DeftypeDecl = "(" "deftype" ( Symbol | "(" Symbol { TypeParam } ")" ) TypeExpr ")" .
```

### `defstruct`

Declares a nominal product type and binds a positional constructor function of the same name:

```ebnf
DefstructDecl = "(" "defstruct" ( Symbol | "(" Symbol { TypeParam } ")" )
                [ "(" { TypeParam } ")" ]
                FieldSpec { FieldSpec } ")" .
FieldSpec     = "(" Symbol TypeExpr ")" .
```

Example:

```lisp
(defstruct Bracket ()
  (floor Money)
  (rate  Float))
```

This binds both the type `Bracket` and the constructor `Bracket : (Fn (Money Float) Bracket)`. Structs can also be constructed by keyword using `(make-struct Bracket :floor #usd"0.00" :rate 0.10)`.

### `=` (Top-Level Constant)

Declares an immutable, statically typed module-level constant or parameter table:

```ebnf
ConstDecl = "(" "=" Symbol TypeExpr Expression ")" .
```

Example:

```lisp
(= std-deduction-2025 (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_SINGLE                        #usd"15750.00"
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY        #usd"31500.00"
    'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY     #usd"15750.00"
    'fed/FILING_STATUS_HEAD_OF_HOUSEHOLD             #usd"23625.00"
    'fed/FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE   #usd"31500.00"))
```

### `def` (Function Declaration)

Declares a statically typed top-level function:

```ebnf
DefDecl   = "(" "def" ( Symbol | "(" Symbol { TypeParam } ")" )
            "(" { ParamSpec } ")" TypeExpr
            { Keyword MetadataExpr }
            Expression { Expression } ")" .
ParamSpec = "(" Symbol TypeExpr ")" .
```

Every parameter must be written as `(paramName ParamType)`, and the return type `TypeExpr` must immediately follow the parameter list. Optional keyword metadata pairs (such as `:cite ("26 U.S.C. § 86")`) may precede the body expression.

---

## Expressions and Special Forms

### Local Bindings: `let`, `with`, `withs`

- **`let`** binds a single local variable (with an optional explicit type annotation):
  ```lisp
  (let x #usd"100.00"
    (+ x #usd"50.00"))

  (let x Money #usd"100.00"
    (+ x #usd"50.00"))
  ```
- **`with`** binds multiple variables in parallel (none of the value expressions are in scope of the others):
  ```lisp
  (with (a #usd"10.00"
         b #usd"20.00")
    (+ a b))
  ```
- **`withs`** binds multiple variables sequentially (each binding is immediately in scope for subsequent bindings, like `let*` in Common Lisp or `withs` in Arc):
  ```lisp
  (withs (gross  #usd"100000.00"
          ded    #usd"15750.00"
          taxable (clamp-zero (- gross ded)))
    taxable)
  ```
  Each binding variable in `with` or `withs` may be either a bare symbol `x` (whose type is inferred from its initializer) or a typed pair `(x Type)`.

### Conditional `if` and Occurrence Typing (Flow Narrowing)

`if` follows Arc's multi-branch alternating syntax without extra inner parentheses:

```ebnf
IfExpr = "(" "if" CondExpr ThenExpr { CondExpr ThenExpr } ElseExpr ")" .
```

Every `if` expression is a value-producing expression and **must** end with an `ElseExpr`. All `CondExpr` must have type `Bool`. If the branches have types $T_1, \dots, T_k$, the `if` expression has the unified type `(Union T1 ... Tk)` (which collapses to $T$ when all branches have the same type $T$).

**Occurrence Typing (Flow Narrowing)**: When a condition expression is:
- `(nil? v)` where `v` has type `(Union T1 ... Tn)`:
  - In the `ThenExpr`, `v` is narrowed to `Nil`.
  - In the `ElseExpr`, `v` is narrowed to `(Union T1 ... Tn)` with `Nil` removed!
- `(isa v TargetType)` where `v` has type `(Union T1 ... Tn)`:
  - In the `ThenExpr`, `v` is narrowed to `TargetType`.
  - In the `ElseExpr`, `v` is narrowed to the remaining union members excluding `TargetType`.

### Exhaustive Union Dispatch: `typecase`

```ebnf
TypecaseExpr = "(" "typecase" "(" Symbol ScrutineeExpr ")"
               TypeExpr BodyExpr { TypeExpr BodyExpr } ")" .
```

`typecase` evaluates `ScrutineeExpr` (which must have a `Union` type), matches its runtime type against each `TypeExpr` branch in order, binds `Symbol` to the narrowed static type `TypeExpr` inside `BodyExpr`, and evaluates the matching `BodyExpr`.

**Compile-Time Exhaustiveness Rule**: Every member of the scrutinee's `UnionType` must be covered by at least one `TypeExpr` branch; otherwise, compilation fails with a non-exhaustive `typecase` error.

Example:

```lisp
(def describe-tin ((tin ret/TaxpayerID)) String
  (typecase (v (. tin 'value))
    String       v
    ret/Withheld "withheld"
    Nil          "unset"))
```

### Anonymous Functions: `fn`

```ebnf
FnExpr = "(" "fn" "(" { ParamSpec } ")" TypeExpr Expression ")" .
```

Constructs a lexically scoped closure of type `(Fn (ParamTypes...) ReturnType)`.

---

## Protocol Buffer Integration

`taxlisp` operates directly on `google.golang.org/protobuf/reflect/protoreflect` descriptors and messages:

1. **Message Construction (`make-proto`)**:
   ```lisp
   (make-proto 'fed/Form1040
     :filing_status 'fed/FILING_STATUS_SINGLE
     :w2_wages wages
     :deduction std
     :taxable_income taxable)
   ```
   The typechecker verifies that `'fed/Form1040` is a valid `MessageDescriptor`, that every `:field_name` exists on `Form1040`, and that each value expression is assignable to the field's static type.

2. **Immutable Copy-on-Write Update (`proto-with`)**:
   ```lisp
   (proto-with existing-1040
     :taxable_income revised-taxable
     :total_tax revised-tax)
   ```
   Clones `existing-1040` without mutating the original message and sets the specified fields.

3. **Dotted Field and `oneof` Access (`.`)**:
   ```lisp
   (. in 'federal.documents.w2s)
   ```
   - Traverses one or more `.`-separated field names across protobuf messages or `defstruct` values.
   - Reading an unset `Money` field returns `#usd"0.00"`; reading an unset `Float` or `Quantity` field returns `0.0`; reading an unset submessage returns an empty default message so chained paths never dereference a null pointer.
   - Reading a `repeated` field of `T` returns `(Slice T)`.
   - Reading a `oneof` group by name (e.g., `(. tin 'value)`) returns the static union `(Union MemberType1 ... MemberTypeN Nil)`.

4. **Field Presence & Oneof Inspection (`has?`, `which-oneof`)**:
   - `(has? msg 'field_name)` returns `Bool` (`true` if the singular field is populated).
   - `(which-oneof msg 'oneof_name)` returns `String` (the active field name, or `""` if unset).

---

## Built-in Operators and Standard Library

### Arithmetic and Dimension Discipline

| Operator | Allowed Signatures | Semantics |
|---|---|---|
| `(+ a b ...)` | `(Money Money...) -> Money`<br>`(Float Float...) -> Float`<br>`(Int Int...) -> Int` | Addition |
| `(- a)` | `(Money) -> Money`<br>`(Float) -> Float`<br>`(Int) -> Int` | Negation |
| `(- a b ...)` | `(Money Money...) -> Money`<br>`(Float Float...) -> Float`<br>`(Int Int...) -> Int` | Subtraction |
| `(* a b)` | `(Money Float) -> Money`<br>`(Float Money) -> Money`<br>`(Float Float) -> Float`<br>`(Int Int) -> Int` | Multiplication |
| `(/ a b)` | `(Money Float) -> Money`<br>`(Money Money) -> Float`<br>`(Float Float) -> Float`<br>`(Int Int) -> Int` | Division |
| `(min a b ...)` | `(Money Money...) -> Money`<br>`(Float Float...) -> Float`<br>`(Int Int...) -> Int` | Minimum value |
| `(max a b ...)` | `(Money Money...) -> Money`<br>`(Float Float...) -> Float`<br>`(Int Int...) -> Int` | Maximum value |

### Rounding and Numeric Conversions

| Function | Signature | Semantics |
|---|---|---|
| `(round-dollar m)` | `(Fn (Money) Money)` | Rounds half-away-from-zero to nearest whole dollar (`$1.00`) |
| `(round-cents m)` | `(Fn (Money) Money)` | Rounds half-away-from-zero to nearest cent (`$0.01`) |
| `(round-rate4 f)` | `(Fn (Float) Float)` | Rounds half-away-from-zero to 4 decimal places (`0.0001`) |
| `(clamp-zero m)` | `(Fn (Money) Money)` | `(max m #usd"0.00")` (IRS "if zero or less, enter -0-") |
| `(abs-money m)` | `(Fn (Money) Money)` | Absolute value `|m|` |
| `(int-to-money n)` | `(Fn (Int) Money)` | Converts integer `n` to `$n.00` |
| `(int-to-float n)` | `(Fn (Int) Float)` | Converts integer `n` to `Float` `n.0` |
| `(money-to-float m)` | `(Fn (Money) Float)` | Converts `Money` dollar value to `Float` |
| `(float-to-money f)` | `(Fn (Float) Money)` | Converts `Float` dollar value to `Money` |

### Comparisons, Dates, and Logic

| Function | Signature | Semantics |
|---|---|---|
| `(< a b)`, `(<= a b)`, `(> a b)`, `(>= a b)` | `Money`, `Float`, `Int`, or `Date` pairs $\to$ `Bool` | Relational ordering |
| `(is a b)`, `(!= a b)` | `(Fn (T U) Bool)` | Value equality / inequality (handles enums, messages, numbers) |
| `(and b1 b2 ...)`, `(or b1 b2 ...)` | `(Bool Bool...) -> Bool` | Short-circuiting boolean conjunction / disjunction |
| `(not b)` | `(Fn (Bool) Bool)` | Boolean negation |
| `(nil? x)` | `(Fn (T) Bool)` | Returns `true` iff `x` is `nil` (narrows union in `if`) |
| `(isa x Type)` | `(x, TypeExpr) -> Bool` | Returns `true` iff `x` matches `TypeExpr` (narrows union in `if`) |
| `(date-year d)`, `(date-month d)`, `(date-day d)` | `(Fn (Date) Int)` | Extracts calendar year, month (1–12), or day (1–31) |

### Slice and Table Combinators

| Function | Signature | Semantics |
|---|---|---|
| `[e1 ...]` / `(slice e1 ...)` | `(T ...) -> (Slice T)` | Constructs a slice from elements |
| `((inst empty-slice T))` | `(Fn () (Slice T))` | Constructs an empty slice of element type `T` |
| `(len xs)` | `(Fn ((Slice T)) Int)` | Number of elements in `xs` |
| `(empty? xs)` | `(Fn ((Slice T)) Bool)` | `true` iff `(is (len xs) 0)` |
| `(elt xs idx)` | `(Fn ((Slice T) Int) T)` | 0-indexed element access |
| `(subslice xs lo hi)` | `(Fn ((Slice T) Int Int) (Slice T))` | Half-open sub-slice `xs[lo:hi]` |
| `(append xs ys)` | `(Fn ((Slice T) (Slice T)) (Slice T))` | Concatenates two slices |
| `(map f xs)` | `(Fn ((Fn (T) U) (Slice T)) (Slice U))` | Maps `f` over each element of `xs` |
| `(keep pred xs)` | `(Fn ((Fn (T) Bool) (Slice T)) (Slice T))` | Retains elements satisfying `pred` |
| `(fold f init xs)` | `(Fn ((Fn (Acc T) Acc) Acc (Slice T)) Acc)` | Left fold over `xs` starting with `init` |
| `(some pred xs)` | `(Fn ((Fn (T) Bool) (Slice T)) Bool)` | `true` if `pred` holds for at least one element |
| `(all pred xs)` | `(Fn ((Fn (T) Bool) (Slice T)) Bool)` | `true` if `pred` holds for all elements |
| `(sum-money xs)` | `(Fn ((Slice Money)) Money)` | Sums a slice of `Money` values |
| `(sum f xs)` | `(Fn ((Fn (T) Money) (Slice T)) Money)` | Sums `(f x)` for each `x` in `xs` |
| `(table k1 v1 ...)` | `(K V ...) -> (Table K V)` | Constructs an immutable lookup table |
| `(lookup tbl k)` | `(Fn ((Table K V) K) (Union V Nil))` | Returns the value for `k`, or `nil` if absent |
| `(lookup-or tbl k default)` | `(Fn ((Table K V) K V) V)` | Returns the value for `k`, or `default` if absent |

---

## Complete Example: IRS Social Security Benefits Worksheet

The following complete `taxlisp` module implements the 18-line **Social Security Benefits Worksheet** (Form 1040 Instructions for Lines 6a and 6b; 26 U.S.C. § 86):

```lisp
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

(= ss-tier1-base (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY      #usd"32000.00"
    'fed/FILING_STATUS_SINGLE                      #usd"25000.00"
    'fed/FILING_STATUS_HEAD_OF_HOUSEHOLD           #usd"25000.00"
    'fed/FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE #usd"25000.00"
    'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY   #usd"0.00"))

(= ss-tier2-spread (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY      #usd"12000.00"
    'fed/FILING_STATUS_SINGLE                      #usd"9000.00"
    'fed/FILING_STATUS_HEAD_OF_HOUSEHOLD           #usd"9000.00"
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
  :cite ("26 U.S.C. § 86" "Form 1040 Instructions, Social Security Benefits Worksheet")

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
```
