# Where these files came from

Every Washington figure in this library is checked against a document the
Washington Department of Revenue published.

Retrieved 2026-08-26, and again on 2026-10-20 with curl 8.5.0. On the second
download the PDF was unchanged, and every extract came out the same except that
one line of `dor-wa-2025-deductions.txt` lost a trailing space. The pages'
HTML hashes are the second download's.

| File | Source | sha256 of the source |
|---|---|---|
| `dor-wa-2025-rate-surcharge.txt` | [Summary of 2025 Legislation](https://dor.wa.gov/sites/default/files/2025-06/2025_Summary_of_Legislation.pdf) | `dc2d76ec1cf9526eeb46003fce831ada400a046d0fb68dd54c8c6d1b81b197d1` |
| `dor-wa-2025-deductions.txt` | [Capital gains tax](https://dor.wa.gov/taxes-rates/other-taxes/capital-gains-tax) | `1171e3944cf7bacca66ad104089a22532dbd874c6d7b790e7cc892b10cfa893c` |
| `rcw-82-87-extract.txt` | [Chapter 82.87 RCW](https://app.leg.wa.gov/rcw/default.aspx?cite=82.87&full=true) | `5d1ba6bc30e2a9b1b8afd98f24918f700edc611355cb28f202b34bf12b4cb3af` |

## Reproducing them

```bash
curl -o wa-2025-legislation.pdf https://dor.wa.gov/sites/default/files/2025-06/2025_Summary_of_Legislation.pdf
pdftotext -layout wa-2025-legislation.pdf waleg.txt   # poppler 24.02.0
awk '/^Capital Gains Tax$/{f=1} /^SSB 5314/{f=0} f' waleg.txt \
  > dor-wa-2025-rate-surcharge.txt

curl -L -o wa-capgains.html https://dor.wa.gov/taxes-rates/other-taxes/capital-gains-tax
sed -e 's/<[^>]*>//g' wa-capgains.html \
  | sed 's/&nbsp;/ /g; s/&amp;/\&/g; s/&#039;/'"'"'/g; s/^[[:space:]]*//' \
  | grep -v '^$' \
  | awk '/^Background$/{f=1} /^Credits$/{f=0} f' \
  > dor-wa-2025-deductions.txt

curl -L -o rcw-82-87.html 'https://app.leg.wa.gov/rcw/default.aspx?cite=82.87&full=true'
cat > rcw2txt.py <<'PY'
# rcw2txt.py PAGE SECTION...: the named sections of an RCW chapter page, one subsection per line.
import html, re, sys
t = html.unescape(re.sub(r"<[^>]+>", "", open(sys.argv[1], encoding="utf-8").read()))
out = []
for body in t.split("PDFRCW")[1:]:
    body = body[: re.search(r"\[ [^\]]*\]", body).end()]
    body = re.sub(r"[ \t\r]*\n\s*", "", body).strip()
    if re.match(r"\d+\.\d+\.\d+", body).group(0) in sys.argv[2:]:
        out.append("RCW " + re.sub(r"(?<=[.;])(?=\(\d+\))", "\n", body))
print("\n\n".join(out))
PY
python3 rcw2txt.py rcw-82-87.html 82.87.020 82.87.040 82.87.050 82.87.060 82.87.070 82.87.080 \
  82.87.100 82.87.110 82.87.115 82.87.120 82.87.150 82.87.160 > rcw-82-87-extract.txt
```

The statute extract is built by finding each section body — introduced by
`PDFRCW`, which the chapter's table of contents entries are not — and taking it
up to the session-law history in brackets that closes it. A new line starts at
each numbered subsection that follows a full stop or a semicolon, so a diff
points at a subsection rather than at a wall of text.

## The statute is checked in because citations go wrong quietly

`TestCitationsMatchTheStatute` requires each subsection this library cites to
contain the words it is cited for. That test was written after it found three
wrong citations in this package, all of which looked right:

| Cited as | Actually | Correct cite |
|---|---|---|
| RCW 82.87.020(1) — standard deduction | Definitions | **82.87.060(1)** |
| RCW 82.87.040(1) — base rate | The subsection exists, the paragraph is finer | **82.87.040(1)(a)** |
| RCW 82.87.040(2) — surcharge | "necessary for the support of the state government" | **82.87.040(1)(b)** |

Every one produced a `Rule.Cite` that a reader checking the working would have
followed to the wrong place. Nothing else in the suite would have noticed,
because the arithmetic was right throughout.

## Why the surcharge threshold is measured after the deduction

RCW 82.87.040(1)(b) taxes "the portion of an individual's Washington capital
gains exceeding $1,000,000", and read alone that could mean the gross gain.

It does not, and the definition settles it. RCW 82.87.020 says "Washington
capital gains" means adjusted capital gain **as modified in RCW 82.87.060** —
the deductions section. So the deductions are inside the defined term, and both
rates apply to the post-deduction figure. The Department describes it the same
way, as "taxable Washington capital gains".

The test asserts that definition verbatim, because it is the one sentence
holding up an interpretation that changes the answer by $29,000 on the example
in the README.

## One source is a web page, which is weaker

The 2.9% surcharge comes from a PDF, and that PDF is a good source: the
Department's own account of the session that enacted it, naming the bill and the
chapter.

The standard deduction does not. RCW 82.87.060(1) creates it and directs the
Department to adjust it for inflation each year, so the statute states a
mechanism and not an amount. The amount for a given year is published on the
Department's capital gains tax page, and that is where `$278,000` comes from.

A web page is a worse oracle than a PDF. It has no revision, no date on its
face, and it can change without anything to diff against. The hash above is of
the HTML as fetched, which lets somebody re-fetch and see *that* it changed but
not what it used to say.

This was the best primary source available. What was checked and rejected: the
statute itself gives no figure; the Department's capital gains overview sheet
(`DORARLCapGainsTax.pdf`, dated July 2025) still prints the 2024 figure of
$270,000; and the return instructions on the site are the 2022 edition. If the
Department later publishes the annual amounts in a dated PDF, this file should
move to it.

The tests are written to depend on the *sentence* rather than the number — they
require "The standard deduction for 2025 is $278,000" verbatim — because the
page carries the 2024 figure in the same paragraph, and a test that merely
looked for `$278,000` somewhere would have passed against the wrong year.

## What this does not prove

That the arithmetic matches Washington's, in the way the California and Oregon
suites do. Those two states publish tables of their own answers, so a test can
compare arithmetic against arithmetic. Washington publishes none, because the
tax is two multiplications and there is nothing to tabulate. What these tests
check is that the rates, the threshold and the deductions are the ones the
Department states, and that the edges behave — the surcharge starting above one
million of *taxable* gain rather than of gross gain, and the charitable
deduction being both floored and capped.

Nothing here covers which gains are Washington's. Allocation under RCW 82.87.100
turns on where the seller was domiciled, and this library returns that question
unanswered rather than guessing.

## The payment rules, retrieved 2026-10-20

These are for `estimated.go` and `estimated_y2026.go`, and
`estimated_test.go` checks every figure against them. Retrieved with curl
8.5.0. The PDFs are extracted with `pdftotext` from poppler 24.02.0 in reading
order. The Department of Revenue's pages are HTML, so there is no pdftotext
step: the tags are stripped as above and the extract is bounded by the page's
own headings. The RCW and WAC sections are the Legislature's PDF prints, each
dated "Certified on" on its face.

| File | Source | sha256 of the source |
|---|---|---|
| `rcw-82-32-090.txt` | [RCW 82.32.090](https://app.leg.wa.gov/RCW/default.aspx?cite=82.32.090&pdf=true), certified 7/15/2026 | `4eb7d3bc868d6eff99d322fd8e383fd87b8c2b74399e50b4568137819f202c00` |
| `rcw-82-32-050.txt` | [RCW 82.32.050](https://app.leg.wa.gov/RCW/default.aspx?cite=82.32.050&pdf=true), certified 7/15/2026 | `bf4536eb3f1aad869c1e65531e3caee489826fdef5bb57e284afb4f479e831ec` |
| `rcw-82-32-105.txt` | [RCW 82.32.105](https://app.leg.wa.gov/RCW/default.aspx?cite=82.32.105&pdf=true) | `4f0a3ac414111eff7583b7f4e62b98c2de9c862eff01fea87d720676f4dac6ee` |
| `rcw-1-16-050.txt` | [RCW 1.16.050](https://app.leg.wa.gov/RCW/default.aspx?cite=1.16.050&pdf=true), certified 8/15/2025 | `aafa8420d99ad5306b940e89c07cbf53b5c700136eed31b6861acd241e30a93c` |
| `rcw-1-12-040.txt` | [RCW 1.12.040](https://app.leg.wa.gov/RCW/default.aspx?cite=1.12.040&pdf=true) | `b046d92a48b279e88af172d47a1ba570dc457365e5fc8ad11a951a79345486c4` |
| `rcw-1-12-070.txt` | [RCW 1.12.070](https://app.leg.wa.gov/RCW/default.aspx?cite=1.12.070&pdf=true) | `d3029b32acf21043f35b394eb4ba8049f9992b3a93a00983cb24f0fb620927b7` |
| `wac-458-20-228.txt` | [WAC 458-20-228](https://app.leg.wa.gov/WAC/default.aspx?cite=458-20-228&pdf=true), certified 2/29/2024 | `38541d3779e3d781e4cac7e7614476f13b79d9a80fa52102eb9bd4250a6dc1b1` |
| `wac-458-20-300.txt` | [WAC 458-20-300](https://app.leg.wa.gov/WAC/default.aspx?cite=458-20-300&pdf=true), certified 8/23/2023 | `ecedd134ba27c6f38a933953db768ece1430fbdd378d223e95eecea7303d1d2a` |
| `ssb-5314-2025.txt` | [SSB 5314, chapter 409, Laws of 2025](https://lawfilesext.leg.wa.gov/biennium/2025-26/Pdf/Bills/Session%20Laws/Senate/5314-S.SL.pdf) | `80367b6c24c3cfaeb5eab5825505ef4924f01cdf6961c955df3388fa92b35ebe` |
| `dor-wa-prepayment-2026.txt` | [Special notice, Prepayment of capital gains tax](https://dor.wa.gov/forms-publications/publications-subject/special-notices/prepayment-capital-gains-tax), issued August 3, 2026 | `db603dda3643c9bfc8c3ea0efd32aecf1c21d446879933721bd0dbe6931c5079` |
| `dor-wa-capital-gains-payments.txt` | [Capital gains tax](https://dor.wa.gov/taxes-rates/other-taxes/capital-gains-tax), "How to report and pay the tax" | `1171e3944cf7bacca66ad104089a22532dbd874c6d7b790e7cc892b10cfa893c` |
| `dor-wa-interim-late-payment-2023.txt` | [Interim statement regarding late payment penalties and Washington's capital gains tax](https://dor.wa.gov/laws-rules/interim-statement-regarding-late-payment-penalties-and-washingtons-capital-gains-tax), April 7, 2023 | `5f036e56fad7ec16bd115758704081b9dbcebeb97ab0d71e7fc1c53d68493be4` |
| `dor-wa-interest-rate-tables.txt` | [Interest rate tables](https://dor.wa.gov/file-pay-taxes/late-filing/interest-rate-tables) | `1ffe4f03e7dc567490ac009f49082ba51844ea7dc1dbb943f9d6277a1c28dc48` |

RCW 82.87.110 and 82.87.115 are read from `rcw-82-87-extract.txt` above, which
already carries 82.87.115 as added by 2026 c 191.

### Reproducing them

```bash
for c in 82.32.090 82.32.050 82.32.105 1.16.050 1.12.040 1.12.070; do
  curl -L -o "rcw-${c//./-}.pdf" "https://app.leg.wa.gov/RCW/default.aspx?cite=$c&pdf=true"
  pdftotext "rcw-${c//./-}.pdf" "rcw-${c//./-}.txt"
done
for c in 458-20-228 458-20-300; do
  curl -L -o "wac-$c.pdf" "https://app.leg.wa.gov/WAC/default.aspx?cite=$c&pdf=true"
  pdftotext "wac-$c.pdf" "wac-$c.txt"
done
curl -L -o 5314-S.SL.pdf 'https://lawfilesext.leg.wa.gov/biennium/2025-26/Pdf/Bills/Session%20Laws/Senate/5314-S.SL.pdf'
pdftotext 5314-S.SL.pdf ssb-5314-2025.txt

strip() {
  sed -e 's/<[^>]*>//g' "$1" \
    | sed 's/&nbsp;/ /g; s/&amp;/\&/g; s/&#039;/'"'"'/g; s/&quot;/"/g; s/^[[:space:]]*//' \
    | grep -v '^$'
}
D=https://dor.wa.gov
curl -L -o prepay.html   "$D/forms-publications/publications-subject/special-notices/prepayment-capital-gains-tax"
curl -L -o capgains.html "$D/taxes-rates/other-taxes/capital-gains-tax"
curl -L -o interim.html  "$D/laws-rules/interim-statement-regarding-late-payment-penalties-and-washingtons-capital-gains-tax"
curl -L -o interest.html "$D/file-pay-taxes/late-filing/interest-rate-tables"
strip prepay.html   | awk '/^Issue Date$/{f=1} /^Questions\?$/{f=0} f' > dor-wa-prepayment-2026.txt
strip capgains.html | awk '/^How to report and pay the tax$/{f=1} /^Questions$/{f=0} f' > dor-wa-capital-gains-payments.txt
strip interim.html  | awk '/^April 7, 2023$/{f=1} /^Taxpayer instructions$/{f=0} f' > dor-wa-interim-late-payment-2023.txt
strip interest.html | awk '/^Contact DORStatistics/{f=1} /^More Information$/{f=0} f' > dor-wa-interest-rate-tables.txt
```

The HTML hashes will not reproduce exactly: the pages carry per-request
tokens. The extracts should.

### Which document wins

Two of the department's documents predate SSB 5314, whose sections 12 and 14
took effect July 27, 2025 (`ssb-5314-2025.txt`, certification page). The
interim statement of April 7, 2023 applies the assessment penalty of RCW
82.32.090(2) to a low estimate, where the act added RCW 82.32.090(10). WAC
458-20-228, certified February 29, 2024, starts interest at the end of January,
where the act starts it at the end of April for this tax. For both, the
statute is computed and the department's document is reported as out of date.
The department's statement that a timely estimate avoids the late payment
penalty is current, and is followed.

### What is not here

- **No published worked example with figures.** WAC 458-20-300 Examples 3 and
  4 state that the filer owes penalties and interest, and no amount. The tests
  move them to 2026's dates.
- **The rate from January 1, 2028.** Not published. The penalty reports the
  gap in `Required`.
- **How interest days are counted.** Neither RCW 82.32.050 nor the department
  says. Interest is figured by the day, and `Required` says so.
- **The 2026 standard deduction.** RCW 82.87.150 requires it by October 31,
  2026, and it was not published on 2026-10-20. It affects the tax, which the
  caller supplies, not the payment rules.
