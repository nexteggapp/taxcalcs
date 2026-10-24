# kansastaxcalc

Kansas estimated individual income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the underpayment
penalty if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Kansas income tax calculator here yet.** The caller supplies the
year's tax, Form K-40 line 18.

## Kansas is close to federal, with a different penalty schedule

| Rule | Kansas | Where |
|---|---|---|
| Who must pay | $500 or more of tax after withholding and credits | [K-40ES](https://www.ksrevenue.gov/pdf/k-40es26.pdf); K.S.A. 79-32,101 |
| The two tests | 90% of this year's tax, or 100% of last year's on a 12-month return. No 110% tier | K-40ES; [Schedule K-210](https://ksrevenue.gov/pdf/k-21025.pdf) lines 2 and 3 |
| No return or no tax last year | Last year's figure is zero, so no penalty | K-210 instructions for lines 1 to 4 |
| Due dates | April 15, June 15, September 15, January 15; filing and paying by January 31 replaces January | K-40ES |
| Withholding | A quarter on each date, with no election to use the actual dates | K-210 line 5 |
| The penalty | At each date, the total still short is charged by the day until the next date or until paid, at 8% for 2026, to April 15 | K-210 Part II; K.S.A. 79-32,107(a), 79-2968 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The forms are followed, and where the statutes differ is reported

Each result's `Divergences` lists five places where the forms and the statutes
read differently: no prior-year return, a prior-year tax under $200, a short
prior year, a late start, and the statute's $1 floor. The figures follow the
forms. The research is in
[`research/estimated-payments/ks.md`](../research/estimated-payments/ks.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
ks, err := kansastaxcalc.NewEstimatedFacts().
    PassThroughCredit(0).          // K-40 line 25, credited like withholding
    RefundableCredits(0).          // K-40 lines 22 and 23, for the $500 test only
    ShortPriorYear(false).         // count a short 2025 return? The K-40ES says no
    LateStartInstallments(false).  // K.S.A. 79-32,103's late start, or K-210's quarters
    JanuaryThirtyFirstMoves(false). // does Sunday, January 31, 2027 move?
    Build()
```

The last three are positions on questions no Kansas document settles. Left
unanswered, each is reported in `Required` with the default used.

## Not built

Farmers and fishers get `estimated.ErrUnsupported`. The local intangibles tax
takes no estimates and is not covered.
