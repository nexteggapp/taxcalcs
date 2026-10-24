# Where these files came from

Every rate in this library is checked against a document the IRS published.
Those documents are PDFs; these files are text extracted from them. This records
which PDF, and how, so that anybody can redo it and diff the result.

Retrieved 2026-08-26 with `pdftotext` from poppler 24.02.0.

| File | Source | sha256 of the PDF |
|---|---|---|
| `rp-2025-32-extract.txt` | [Rev. Proc. 2025-32](https://www.irs.gov/pub/irs-drop/rp-25-32.pdf) | recorded when 2026 was added |
| `rp-2024-40-extract.txt` | [Rev. Proc. 2024-40](https://www.irs.gov/pub/irs-drop/rp-24-40.pdf) | `4de9db6b6662b5c305a59100a027051628823b42fd5817ea4e383ad4c302af1f` |
| `i1040-2025-tax-table.txt` | [2025 Form 1040 instructions](https://www.irs.gov/pub/irs-pdf/i1040gi.pdf) | `482e9c487c608f1bbeaceef35bc3c0933e8b35443cfff447e4279d590468364a` |
| `i1040-2025-standard-deduction.txt` | same, plus [2025 Schedule SE](https://www.irs.gov/pub/irs-pdf/f1040sse.pdf) | `05bc2b3e...` for the Schedule SE |
| `p505-2026-safe-harbour-example.txt` | [Publication 505 (2026)](https://www.irs.gov/pub/irs-pdf/p505.pdf) | `c69e87caa93cdefa4493f0ec1d8478b1ff9ec1329ee18c7b9d442f2dcb79c621` |

## Reproducing the tax table extract

```bash
curl -o i1040gi.pdf https://www.irs.gov/pub/irs-pdf/i1040gi.pdf
pdftotext -layout i1040gi.pdf i1040gi.txt
awk '/Married Married Head of.*Married Married Head of/{f=1} /2025 Tax Computation Worksheet/{f=0} f' i1040gi.txt \
  | grep -E "^ *[0-9][0-9,]* +[0-9][0-9,]* +[0-9,]+ +[0-9,]+ +[0-9,]+ +[0-9,]+" \
  > i1040-2025-tax-table.txt
```

The bounds are the document's own headings rather than line numbers. That is not
cosmetic: an earlier version used a line range and clipped the last row of the
table, and a second version anchored on the *Sample Table* heading and pulled in
the illustrative row the instructions print to show you how to read the table —
a real row with real figures, which passed every value check and showed up only
as a band overlapping its neighbour.

## Why extract at all

Extraction makes assumptions and it is brittle. The alternative is worse.

Transcribing by hand has the same brittleness with no way to check it. Working
from memory is worse again, and demonstrably so: writing this library from
recollection would have put the 2025 standard deduction on Form 1040 line 12,
the SALT cap at $10,000, adjusted gross income on line 11, and would not have
known Schedule 1-A exists. The printed forms say 12e, $40,000, 11a, and it does.

So the extract stays, on three conditions, all of which the tests enforce:

**The extract is checked in, not fetched.** A test failure means this library
changed, never that the IRS republished or that poppler shipped a new version.
Refreshing a file is a deliberate commit.

**Completeness is asserted, not assumed.** `assertTableIsComplete` requires the
tax table extract to run from $0 to $100,000 with every band's floor equal to
the previous band's ceiling. A count would not do: an earlier version asserted
only "at least 1,900 bands" and passed while missing eighty of them between
$94,000 and $99,000, two whole page columns dropped because the table's last
page has a short third column.

**One gap is allowed, named, and no others.** Below $25 the printed rows are too
narrow to survive extraction, so the $5–$15 and $15–$25 bands are missing. The
tax there is $1 and $2. Any other discontinuity fails the build.

## What this still does not prove

That the extract faithfully represents the PDF. The hashes let somebody redo the
extraction and diff, and nothing runs that check automatically. Verifying it
means fetching the PDF and re-running the commands above.

The figures themselves are checked, which is the thing that matters more: the
brackets and thresholds are compared line by line against the revenue procedure,
and the tax table is compared against this library's own arithmetic for every
band it covers. A scrape that lost or garbled a figure would have to produce one
that happens to agree with an independent calculation to get through.

## Estimated tax for 2026

The figures in `estimated_y2026.go`, checked by `estimated_test.go`. Retrieved
2026-10-20 with curl 8.5.0 and extracted with `pdftotext` from poppler 24.02.0.

| File | Source | sha256 of the PDF |
|---|---|---|
| `f1040es-2026-general-rule.txt` | [2026 Form 1040-ES](https://www.irs.gov/pub/irs-pdf/f1040es.pdf), page 1 | `396ffad25baa737b33a67d27624f7444be3ac4e7245d287aebe5f89864f90b54` |
| `f1040es-2026-due-dates.txt` | same, Payment Due Dates | same |
| `f2210-2025.txt` | [2025 Form 2210](https://www.irs.gov/pub/irs-pdf/f2210.pdf), whole form | `6899ce672648b280bf00ab47200f1b0fbf40368cfbf137df507b945b8577159a` |
| `i2210-2025-general.txt` | [2025 Instructions for Form 2210](https://www.irs.gov/pub/irs-pdf/i2210.pdf), page 1 | `7bd6a47494e588fcb422a9ae97970a369fe24fd778ad33b224aeb1cd759e831d` |
| `i2210-2025-examples.txt` | same, line 10 through the penalty worksheet, reading order | same |
| `i2210-2025-penalty-worksheet.txt` | same, the same span, page layout | same |
| `rr-2026-5.txt` | Rev. Rul. 2026-5 in [I.R.B. 2026-8](https://www.irs.gov/pub/irs-irbs/irb26-08.pdf) | `ac3604bf1e9fdc5714a6201e6afa287a5b83b7b0553902033f7a163d73cfc0bc` |
| `rr-2026-10.txt` | Rev. Rul. 2026-10 in [I.R.B. 2026-22](https://www.irs.gov/pub/irs-irbs/irb26-22.pdf) | `ee364d94f3b589274e36cdc7ce6bd9cb3c7a6c3d13599ccce297e2aef62c55ae` |
| `rr-2026-15.txt` | [Rev. Rul. 2026-15](https://www.irs.gov/pub/irs-drop/rr-26-15.pdf) | `025d2ab1cc5bfddfc4d3e17a0bffbbf79fe13f28026d852c36356f8d91a307f1` |
| `p509-2026-legal-holidays.txt` | [Publication 509 (2026)](https://www.irs.gov/pub/irs-pdf/p509.pdf), Legal holidays | `d7d7e3f816bb3e08782d4628efeee1505ae9740fa0a4633aa8746f02f3f4a0cb` |

Rev. Rul. 2026-5 and 2026-10 have no standalone PDF under `irs-drop/`; the
rulings are taken from the bulletins that published them. The first quarter's
ruling, Rev. Rul. 2025-22, is not checked in: its rate never applies to a 2026
underpayment, which starts on April 16, 2026.

### Reproducing them

```bash
curl -o f1040es.pdf https://www.irs.gov/pub/irs-pdf/f1040es.pdf
curl -o f2210.pdf   https://www.irs.gov/pub/irs-pdf/f2210.pdf
curl -o i2210.pdf   https://www.irs.gov/pub/irs-pdf/i2210.pdf
curl -o irb26-08.pdf https://www.irs.gov/pub/irs-irbs/irb26-08.pdf
curl -o irb26-22.pdf https://www.irs.gov/pub/irs-irbs/irb26-22.pdf
curl -o rr-26-15.pdf https://www.irs.gov/pub/irs-drop/rr-26-15.pdf
curl -o p509.pdf    https://www.irs.gov/pub/irs-pdf/p509.pdf

pdftotext f1040es.pdf f1040es.raw.txt
awk 'NR==1,/^Additional Information You May Need$/' f1040es.raw.txt > f1040es-2026-general-rule.txt
awk '/^Payment Due Dates$/{f=1} f; /Pub. 509, Tax Calendars, for a list/{if(f)exit}' f1040es.raw.txt > f1040es-2026-due-dates.txt

pdftotext -layout f2210.pdf f2210-2025.txt

pdftotext i2210.pdf i2210.raw.txt
pdftotext -layout i2210.pdf i2210.txt
awk 'NR==1,/^The IRS Will Figure the Penalty for/' i2210.raw.txt > i2210-2025-general.txt
awk '/^Line 10$/{f=1} f; /^Line 4\. Make the computation requested/{if(f)exit}' i2210.raw.txt > i2210-2025-examples.txt
awk '/Enter on line 10, columns \(a\) through \(d\)/{f=1} f; /^Line 4\. Make the computation requested/{if(f)exit}' i2210.txt > i2210-2025-penalty-worksheet.txt

pdftotext irb26-08.pdf irb26-08.raw.txt
pdftotext irb26-22.pdf irb26-22.raw.txt
awk '/^Rev\. Rul\. 2026-5$/{f=1} f; /^DRAFTING INFORMATION$/{if(f)exit}' irb26-08.raw.txt > rr-2026-5.txt
awk '/^Rev\. Rul\. 2026-10$/{f=1} f; /^DRAFTING INFORMATION$/{if(f)exit}' irb26-22.raw.txt > rr-2026-10.txt
pdftotext -layout rr-26-15.pdf rr-26-15.txt
awk '{print} /DRAFTING INFORMATION/{exit}' rr-26-15.txt > rr-2026-15.txt

pdftotext p509.pdf p509.raw.txt
awk '/Legal holidays occurring in/{f=1} f; /December 25—Christmas Day/{if(f)exit}' p509.raw.txt > p509-2026-legal-holidays.txt
```

Two extraction modes are used on purpose. Prose set in two columns is
interleaved line by line by `-layout`, so a sentence such as Example 3 of the
Form 2210 instructions cannot be found in it; reading order keeps it whole.
Tables lose their rows in reading order, so Table 2 of the same instructions,
the chart of days per rate period, is read from the `-layout` extract of the
same span. The tests collapse whitespace before matching either.

### What is not here

- **The 2026 Form 2210 and its instructions** are not published. The 2025
  editions are used for what does not change between years: the line
  arithmetic, the payment application rule, the Schedule AI periods and
  percentages, and the January 31 and April 15 dates a year on.
- **The rate for January 1 to April 15, 2027** is set by a ruling due in
  November 2026. The penalty reports the gap in `Required`.
- **Publication 509 (2027)** is not published, so 2027 has no holiday list.
  The 2027 dates the 1040-ES prints are weekdays and the tests check the
  computed dates against them.

## Capital gains and the gains-rate worksheet

Retrieved 2026-10-20 with `pdftotext -layout` from poppler 24.02.0. Each file
is a span of the extracted text, named by its printed lines.

| File | Source | sha256 of the PDF |
|---|---|---|
| `f1040sd-2025-lines.txt` | [2025 Schedule D (Form 1040)](https://www.irs.gov/pub/irs-pdf/f1040sd.pdf), lines 6, 7, 13 to 17 and 21 | `90564c8b7e49280363612639b804d113ebedb516c2e87c70649f29c844da1d2e` |
| `i1040sd-2025-capital-losses.txt` | [2025 Instructions for Schedule D](https://www.irs.gov/pub/irs-pdf/i1040sd.pdf), Capital Losses, the right-hand column | `650643a686378b7573e4f4476ee4e7c9f9bab1384314dca32dc336f7b2276002` |
| `i1040-2025-qdcg-worksheet.txt` | [2025 Form 1040 instructions](https://www.irs.gov/pub/irs-pdf/i1040gi.pdf), Qualified Dividends and Capital Gain Tax Worksheet, lines 1 to 10 | `482e9c487c608f1bbeaceef35bc3c0933e8b35443cfff447e4279d590468364a`, the PDF the tax table came from |

```bash
pdftotext -layout f1040sd.pdf f1040sd.txt
{ sed -n 36,39p f1040sd.txt; sed -n 62,66p f1040sd.txt; sed -n 70,81p f1040sd.txt; sed -n 98,104p f1040sd.txt; } > f1040sd-2025-lines.txt
pdftotext -layout i1040sd.pdf i1040sd.txt
sed -n 186,197p i1040sd.txt | cut -c100- > i1040sd-2025-capital-losses.txt
pdftotext -layout i1040gi.pdf i1040gi.txt
sed -n 2455,2484p i1040gi.txt > i1040-2025-qdcg-worksheet.txt
```

The $3,000 and $1,500 limits are § 1211(b)(1) and are not adjusted for
inflation, so the 2025 documents state the figure for 2026 too.

## The traditional IRA deduction

Retrieved 2026-10-20 with plain `curl`, no user agent set. The notice is
extracted with `pdftotext -layout` from poppler 24.02.0. The statute is reduced
from the Government Publishing Office's HTML to one line per block, with the
`html2text.py` script in `minnesotataxcalc/testdata/SOURCES.md`.

| File | Source | sha256 |
|---|---|---|
| `n-2025-67-ira.txt` | [Notice 2025-67](https://www.irs.gov/pub/irs-drop/n-25-67.pdf), 2026 amounts relating to retirement plans and IRAs: the paragraphs on § 219(b)(5) and § 219(g) | `1eea8f141b0cddd182f9f09b3bc8ffad683d27ceb806dfc6da126811dc0a1f8d` (the PDF) |
| `usc26-219g-2024.txt` | [26 U.S.C. § 219, 2024 edition](https://www.govinfo.gov/content/pkg/USCODE-2024-title26/html/USCODE-2024-title26-subtitleA-chap1-subchapB-partVII-sec219.htm), subsection (g) paragraphs (1) to (3) and (7) | `8e46f501ac8b8c520abf2795f1afc5e959309f73d6e146c75b14eb03e120d547` (the HTML) |

```bash
curl -o n-25-67.pdf https://www.irs.gov/pub/irs-drop/n-25-67.pdf
pdftotext -layout n-25-67.pdf n.txt
awk '/^The deductible amount under section 219\(b\)\(5\)\(A\)/{f=1} f{print} /remains \$0 to \$10,000\./{if(f)exit}' n.txt > n-2025-67-ira.txt

curl -o 219.htm "https://www.govinfo.gov/content/pkg/USCODE-2024-title26/html/USCODE-2024-title26-subtitleA-chap1-subchapB-partVII-sec219.htm"
python3 html2text.py 219.htm > 219.txt
{ awk '/^\(g\) Limitation on deduction for active participants/{f=1} /^\(4\) Special rule for married individuals filing separately/{if(f)exit} f' 219.txt
  awk '/^\(7\) Special rule for spouses who are not active participants/{f=1} /^\(8\) Inflation adjustment/{if(f)exit} f' 219.txt; } > usc26-219g-2024.txt
```

The notice gives the 2026 limit, the age-50 addition and the four phase-out
ranges. The statute gives the arithmetic the notice does not: the reduction is
in proportion across the range, it is rounded to the next lowest $10, and it
leaves no less than $200 until the deduction reaches zero. The 2024 edition is
the latest the GPO serves. The notice's 2026 ranges are $10,000 wide, or
$20,000 on a joint return where the contributor is covered, which is the
statute's (g)(2)(A)(ii) and (g)(7)(B). Uscode.house.gov returned "Page Not
Found" for the section.

## Roth IRA contributions and the 6% tax on excess contributions

Retrieved 2026-10-21 with plain `curl`, no user agent set. The notice is the
same PDF as above. The statutes are reduced with the same `html2text.py`.

| File | Source | sha256 |
|---|---|---|
| `n-2025-67-roth.txt` | [Notice 2025-67](https://www.irs.gov/pub/irs-drop/n-25-67.pdf), the paragraphs on § 408A(c)(3) | `1eea8f141b0cddd182f9f09b3bc8ffad683d27ceb806dfc6da126811dc0a1f8d` (the PDF) |
| `usc26-408a-c-2024.txt` | [26 U.S.C. § 408A, 2024 edition](https://www.govinfo.gov/content/pkg/USCODE-2024-title26/html/USCODE-2024-title26-subtitleA-chap1-subchapD-partI-subpartA-sec408A.htm), subsection (c) paragraphs (1) to (3)(C) | `66d9f2d55abe8966dd6be370886eebc7ee85a85513282e8e0dcdb5699368247c` (the HTML) |
| `usc26-4973-2024.txt` | [26 U.S.C. § 4973, 2024 edition](https://www.govinfo.gov/content/pkg/USCODE-2024-title26/html/USCODE-2024-title26-subtitleD-chap43-sec4973.htm), subsections (a), (b) and (f) | `e8ab75309b2b0d959546cd14c510a48e071ef4ba82c8fa06b19b217a6153cc6d` (the HTML) |

```bash
awk '/^The adjusted gross income limitation under section 408A\(c\)\(3\)\(B\)\(ii\)\(I\) for/{f=1} f{print} /between \$0 and \$10,000\./{if(f)exit}' n.txt > n-2025-67-roth.txt

curl -o 408A.htm "https://www.govinfo.gov/content/pkg/USCODE-2024-title26/html/USCODE-2024-title26-subtitleA-chap1-subchapD-partI-subpartA-sec408A.htm"
curl -o 4973.htm "https://www.govinfo.gov/content/pkg/USCODE-2024-title26/html/USCODE-2024-title26-subtitleD-chap43-sec4973.htm"
python3 html2text.py 408A.htm > 408A.txt
python3 html2text.py 4973.htm > 4973.txt
awk '/^\(c\) Treatment of contributions/{f=1} /^\(D\) Inflation adjustment/{if(f)exit} f' 408A.txt > usc26-408a-c-2024.txt
{ awk '/^\(a\) Tax imposed/{f=1} /^\(c\) Section 403\(b\) contracts/{if(f)exit} f' 4973.txt
  awk '/^\(f\) Excess contributions to Roth IRAs/{f=1} /^\(g\) Excess contributions to health savings accounts/{if(f)exit} f' 4973.txt; } > usc26-4973-2024.txt
```

The notice gives the 2026 Roth ranges. Its start for a joint return, $242,000,
also applies to a qualifying widow(er). The statute gives the rest:

- the Roth limit is the § 219 limit less traditional contributions;
- the range is $15,000 wide, or $10,000 on a joint or separate return;
- § 219(g)(2)'s rounding and $200 floor apply.

A qualifying widow(er) files neither a joint nor a separate return, so the
range is $242,000 to $257,000. The notice does not print that range.

Section 4973 gives the 6% tax and what counts as an excess.

## Where the 6% tax on excess contributions goes

Retrieved 2026-10-22 with plain `curl`, no user agent set, and extracted with
`pdftotext -layout` from poppler 24.02.0.

| File | Source | sha256 of the PDF |
|---|---|---|
| `f5329-2025-excess.txt` | [2025 Form 5329](https://www.irs.gov/pub/irs-pdf/f5329.pdf), Parts III and IV | `2ad77023aebb280c90e4083c29c931b4f0fc9fc6264183e812d2361ba7e916bb`, the PDF Part I came from |
| `f1040s2-2025-lines-8-21.txt` | [2025 Schedule 2 (Form 1040)](https://www.irs.gov/pub/irs-pdf/f1040s2.pdf), lines 8 and 21 | `64d867b683334cfc533993e37ccb0d143449ec29428a78206be2e4de333941e8` |
| `f1040-2025-lines-23-24.txt` | [2025 Form 1040](https://www.irs.gov/pub/irs-pdf/f1040.pdf), lines 23 and 24 | `3d31c226df0d189ced80e039d01cf0f8820c1019681a0f0ca6264de277b7e982` |

```bash
curl -o f1040s2.pdf https://www.irs.gov/pub/irs-pdf/f1040s2.pdf
curl -o f1040.pdf https://www.irs.gov/pub/irs-pdf/f1040.pdf
pdftotext -layout f5329.pdf f5329.txt
pdftotext -layout f1040s2.pdf f1040s2.txt
pdftotext -layout f1040.pdf f1040.txt
awk '/^Part III +Additional Tax on Excess Contributions to Traditional IRAs/{f=1} f{print} /\(including 2025 contributions made in 2026\)\. Include this amount on Schedule 2 \(Form 1040\), line 8 \. \. \.  /{if(f)exit}' f5329.txt > f5329-2025-excess.txt
{ awk '/^  8     Additional tax on IRAs or other tax-favored accounts/{f=1} f{print} /If not required, check here/{if(f)exit}' f1040s2.txt
  awk '/^ 21     Add lines 4, 7 through 16, 18, and 19/{f=1} f{print} /or 1040-SR, line 23; or Form 1040-NR, line 23b/{if(f)exit}' f1040s2.txt; } > f1040s2-2025-lines-8-21.txt
sed -n 112,113p f1040.txt > f1040-2025-lines-23-24.txt
```

Form 5329 lines 17 and 25 put the tax on Schedule 2 line 8. Line 21 carries
it to Form 1040 line 23, and line 24 adds it into total tax. Form 2210 line 2,
in `i2210-2025-line-2.txt`, takes Schedule 2 line 8 for "additional tax on
distributions only". So the 6% tax is in total tax but not in the tax that
sets the required annual payment.

## Additional tax on early distributions, and state tax refunds

Retrieved 2026-10-20 with plain `curl`, no user agent set, and extracted with
`pdftotext -layout` from poppler 24.02.0.

| File | Source | sha256 of the PDF |
|---|---|---|
| `i2210-2025-line-2.txt` | [2025 Instructions for Form 2210](https://www.irs.gov/pub/irs-pdf/i2210.pdf), Line 2, the right-hand column | `7bd6a47494e588fcb422a9ae97970a369fe24fd778ad33b224aeb1cd759e831d`, the PDF the other extracts came from |
| `i2210-2025-waiver.txt` | [2025 Instructions for Form 2210](https://www.irs.gov/pub/irs-pdf/i2210.pdf), Waiver of Penalty, the left-hand column | `7bd6a47494e588fcb422a9ae97970a369fe24fd778ad33b224aeb1cd759e831d`, the same PDF |
| `i5329-2025-code-1.txt` | [2025 Instructions for Form 5329](https://www.irs.gov/pub/irs-pdf/i5329.pdf), Who Must File, the code 1 bullet | `aec3d325edb26a0d20d3b81030feb97cf62ecc97fa90c9abd91d83dd22749146` |
| `f5329-2025-part-i.txt` | [2025 Form 5329](https://www.irs.gov/pub/irs-pdf/f5329.pdf), Part I | `2ad77023aebb280c90e4083c29c931b4f0fc9fc6264183e812d2361ba7e916bb` |
| `i1040-2025-state-refund-worksheet.txt` | [2025 Form 1040 instructions](https://www.irs.gov/pub/irs-pdf/i1040gi.pdf), State and Local Income Tax Refund Worksheet—Schedule 1, Line 1 | `482e9c487c608f1bbeaceef35bc3c0933e8b35443cfff447e4279d590468364a`, the PDF the tax table came from |

```bash
pdftotext -layout i2210.pdf i2210.txt
sed -n 111,141p i2210.txt | cut -c80- > i2210-2025-line-2.txt
sed -n 100,107p i2210.txt | cut -c1-79 > i2210-2025-waiver.txt
pdftotext -layout i5329.pdf i5329.txt
sed -n 39,48p i5329.txt | cut -c1-70 > i5329-2025-code-1.txt
pdftotext -layout f5329.pdf f5329.txt
sed -n 19,31p f5329.txt > f5329-2025-part-i.txt
pdftotext -layout i1040gi.pdf i1040gi.txt
sed -n 6509,6556p i1040gi.txt > i1040-2025-state-refund-worksheet.txt
```

Form 2210 line 2 counts Schedule 2 line 8's "additional tax on distributions
only", so the 10% additional tax on an early distribution is part of the tax
the estimated tax rules measure. The Form 5329 instructions let a filer whose
Forms 1099-R all show code 1, owing the tax on the full amount, report 10% on
Schedule 2 line 8 directly; that is the case `EarlyDistributionTax` computes.

[Publication 525 (2025)](https://www.irs.gov/pub/irs-pdf/p525.pdf) (sha256 `39b0fa5e04c5d159a34583da12a91326f396f69286f4b7a6f399d34cebc2c25b`) sends Form 1040 filers to the worksheet in the Form
1040 instructions for a refund of last year's state or local income tax,
except in the cases its Itemized Deduction Recoveries section lists. The
worksheet is used for 2026 with 2025's standard deduction on lines 5 and 6,
as the 2025 worksheet uses 2024's.

## The 2026 limits on itemised deductions

Retrieved 2026-10-21 with plain `curl`, no user agent set, from the
Government Publishing Office: [Public Law 119-21](https://www.govinfo.gov/content/pkg/PLAW-119publ21/html/PLAW-119publ21.htm),
sha256 of the HTML `84000b07c382db50ead04ce65b70983e2dcd699a931d80cd3d2edac584e9004d`.
The HTML's tags are stripped and the entities unescaped; each file is a span
of the result, named by section.

| File | Holds |
|---|---|
| `plaw-119-21-sec-70111.txt` | Sec. 70111: § 68 as amended, the 2/37 limitation, and its effective date |
| `plaw-119-21-sec-70425.txt` | Sec. 70425(a)(1): the 0.5% floor in § 170(b)(1)(I), and the section's effective date |

```bash
curl -o plaw.htm "https://www.govinfo.gov/content/pkg/PLAW-119publ21/html/PLAW-119publ21.htm"
python3 -c 'import re,html,sys; t=open("plaw.htm").read(); sys.stdout.write(html.unescape(re.sub(r"<[^>]+>","",t)))' > plaw.txt
awk 'NR>=6012 && NR<=6045' plaw.txt > plaw-119-21-sec-70111.txt
{ awk 'NR>=10664 && NR<=10679' plaw.txt; awk 'NR>=10784 && NR<=10786' plaw.txt; } > plaw-119-21-sec-70425.txt
```

The contribution base for the 0.5% floor is taken as adjusted gross
income. The definition of the contribution base is not checked in, so any
difference between the two is not handled.

## Distribution codes, the rental allowance and excess Social Security

Retrieved 2026-10-21 with plain `curl`, no user agent set; PDFs extracted with
`pdftotext -layout` from poppler 24.02.0, the statute reduced with the
`html2text.py` script in `minnesotataxcalc/testdata/SOURCES.md`.

| File | Source | sha256 |
|---|---|---|
| `i1099r-2026-codes.txt` | [2026 Instructions for Forms 1099-R and 5498](https://www.irs.gov/pub/irs-pdf/i1099r.pdf), Table 1, Guide to Distribution Codes | `0bb009120b7793c15445c0e4be2cd525bc37b19c215f2478b6df627dcf23db14` |
| `i1099r-2026-direct-rollover.txt` | same, Reporting a direct rollover | same |
| `i1040-2025-rollovers.txt` | [2025 Form 1040 instructions](https://www.irs.gov/pub/irs-pdf/i1040gi.pdf), Rollovers | `482e9c487c608f1bbeaceef35bc3c0933e8b35443cfff447e4279d590468364a` |
| `usc26-469i-2024.txt` | [26 U.S.C. § 469, 2024 edition](https://www.govinfo.gov/content/pkg/USCODE-2024-title26/html/USCODE-2024-title26-subtitleA-chap1-subchapE-partII-subpartC-sec469.htm), (i)(1) to (3) and (5) | `91ccd3c2936c45f9a60da9fbfe2c0fcba06353f7dd755ac0574a4d4af625f175` (the HTML) |
| `i2210-2025-line-6.txt` | 2025 Instructions for Form 2210, Line 6 and the payment rules for line 11 | `7bd6a47494e588fcb422a9ae97970a369fe24fd778ad33b224aeb1cd759e831d` |
| `f1040s3-2025-line-11.txt` | [2025 Schedule 3 (Form 1040)](https://www.irs.gov/pub/irs-pdf/f1040s3.pdf), line 11 | `008cfd3fe3ebd0860452069b90fd0a3d53dda6dd8a5d870278370aa899a63f82` |

```bash
pdftotext -layout i1099r.pdf i1099r.txt
{ sed -n 1206,1330p i1099r.txt; sed -n 1350,1395p i1099r.txt; } > i1099r-2026-codes.txt
sed -n 340,352p i1099r.txt | cut -c1-66 > i1099r-2026-direct-rollover.txt
sed -n 1906,1935p i1040gi.txt | cut -c94-160 > i1040-2025-rollovers.txt
python3 html2text.py 469.htm > 469.txt
{ awk '/^\(i\) \$25,000 offset for rental real estate activities/{f=1} /^\(4\) Special rule for estates/{if(f)exit} f' 469.txt
  awk '/^\(5\) Married individuals filing separately/{f=1} /^\(6\) Active participation/{if(f)exit} f' 469.txt; } > usc26-469i-2024.txt
{ sed -n 213,218p i2210.txt | cut -c1-80; sed -n 258,268p i2210.txt | cut -c80-175; } > i2210-2025-line-6.txt
pdftotext -layout f1040s3.pdf s3.txt; sed -n 40p s3.txt > f1040s3-2025-line-11.txt
```

The codes handled are those whose 2026 treatment these documents settle: 1,
2, 3, 4, 7, G, H and P. Code A's 10-year option (Form 4972), Roth and
designated Roth distributions (B, J, Q, T), SIMPLE IRAs in their first two
years (S), corrective distributions (8) and the rest are not worked out.
§ 469 is the GPO's 2024 edition, the latest it serves.
