# Where these files came from

The Legislature's figures in this package are checked against text extracted
from the statutes it publishes. Treasury's figures are checked against the
research file's verified quotes, because www.michigan.gov refused the
downloads (see below). This records which document, and how, so that anybody
can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. HTML pages were reduced to text with
the Python 3.12.3 script below.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `mcl-206-301.txt` | [MCL 206.301](https://www.legislature.mi.gov/Laws/MCL?objectName=mcl-206-301), the section text | `cc249368c8021764c9430ec240455009515448df8faa318fc5710983eeaf22cd` (the HTML) |
| `mcl-205-23.txt` | [MCL 205.23](https://www.legislature.mi.gov/Laws/MCL?objectName=mcl-205-23) | `c1653be7a481785f37b92ebc76578c551fe77bcaa926af6db7f52f60f0a45316` (the HTML) |
| `mcl-205-24.txt` | [MCL 205.24](https://www.legislature.mi.gov/Laws/MCL?objectName=mcl-205-24) | `6525814c57108ba0d3576bcb2532e51a77e5f5e13a50109b049f379417fa6713` (the HTML) |
| `mcl-8-6.txt` | [MCL 8.6](https://www.legislature.mi.gov/Laws/MCL?objectName=mcl-8-6) | `bd6054fb84bbe5fe52e9b0198683a3536aece20c244556c916eef10385985143` (the HTML) |
| `research-mi.md` | `research/estimated-payments/mi.md` at commit `31d590e18`, copied unchanged. **Not a primary source**: it stands in for Treasury's documents, which could not be fetched | `567b754dc7d1aa1cb0f1b16992935265bcaa7fd5d13f41028feab31f15c1e943` |

An HTML page's hash records what was fetched. Pages carry navigation and
scripts that change without their content changing, so a later fetch can hash
differently and extract the same; diff the extract rather than the hash. The
statute pages say the compilation is complete through PA 103 of 2026.

## Reproducing them

```bash
for s in 206-301 205-23 205-24 8-6; do
  curl -o mcl-$s.html "https://www.legislature.mi.gov/Laws/MCL?objectName=mcl-$s"
done

cat > html2text.py <<'PY'
# Reduces an HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
for s in 206-301 205-23 205-24 8-6; do
  python3 html2text.py mcl-$s.html | awk '/\(EXCERPT\)$/{f=1} f; /^History:/{if(f)exit}' > mcl-$s.txt
done

cp ../../research/estimated-payments/mi.md research-mi.md
```

## Treasury's documents were blocked

Every www.michigan.gov address answered HTTP 403 with an Akamai page headed
"Access Denied" to a plain `curl -L -o` on 2026-10-20:

- `https://www.michigan.gov/taxes/-/media/Project/Websites/taxes/Forms/IIT/TY2025/MI-1040ES-(2026).pdf`,
  the 2026 MI-1040ES (Rev. 04-25);
- `https://www.michigan.gov/taxes/-/media/Project/Websites/taxes/Forms/IIT/TY2025/MI-2210.pdf`,
  the 2025 MI-2210 (Rev. 10-25) with its instructions;
- `https://www.michigan.gov/taxes/-/media/Project/Websites/taxes/Forms/IIT/TY2024/MI-1040.pdf`,
  the 2024 MI-1040;
- `https://www.michigan.gov/taxes/rep-legal/rab/rabhtml/2021/revenue-administrative-bulletin-2021-17`,
  RAB 2021-17, Estimated Payments for Individuals and Fiduciaries;
- `https://www.michigan.gov/taxes/rep-legal/rab/2026-revenue-administrative-bulletins/revenue-administrative-bulletin-2026-5`,
  RAB 2026-5, the interest rate for July 1 to December 31, 2026;
- `https://www.michigan.gov/taxes/rep-legal/rab/2026-revenue-administrative-bulletins/revenue-administrative-bulletin-2026-10`,
  RAB 2026-10, the interest rate for January 1 to June 30, 2027;
- `https://www.michigan.gov/taxes/rep-legal/rab/2025-revenue-administrative-bulletins/revenue-administrative-bulletin-2025-15`,
  RAB 2025-15, Penalty Provisions;
- `https://www.michigan.gov/treasury/reference/taxpayer-notices/2026/03/19/march-2026-extreme-winter-weather-notice`,
  Treasury's notice of March 19, 2026, extreme winter weather.

These figures from them are checked against the quote the research file's
verification read in the document itself, through `research-mi.md`: the $500
threshold, the 100% and 110% tests, the $150,000 and $75,000 steps, the 12-month
return, the 110% rule not applying to farmers and seafarers, the four due
dates, the June 16, 2025 weekend shift, residents and nonresidents alike, the
January 31 and February 1, 2027 rule, a quarter of withholding on each date,
the $500 or less exception, the no-liability exception, no penalty when
estimates were not required last year, annualising all four columns, the
0.0002150 and 0.0002129 daily factors with 7.85% and 7.77%, the 25% and 10%
penalties with their $25 and $10 minimums, no carrying back, the fourth
quarter's payments through January 15 and not after January 16, and disaster
relief waiving penalty and interest. That
checks this package against the research, not against Treasury.

These checks were dropped, because the research quotes nothing for the figure:

- the 90% test and 66 2/3% for farmers, fishermen and seafarers (MI-1040ES;
  MI-2210 line 6);
- the smaller of lines 4 and 6, and line 7 divided by four (MI-2210 lines 7
  and 9; the quarter is still checked against MCL 206.301(1));
- the rate reset on January 1 and July 1 (MI-1040ES; the prime-plus-one rate
  is still checked against MCL 205.23(2));
- "by February 1, 2027, instead of filing an MI-1040ES", the MI-1040ES's
  second wording of the January rule;
- the two-thirds farming, fishing or seafaring test (MI-1040ES; MI-2210);
- the MI-1040ES chart of income changes and first due dates, which sets the
  April 1, June 1 and September 1 rows;
- the MI-2210 date headings other than June 16, 2025;
- January 1 to June 30, 2026 at 8.48%, 0.0002324 a day (MI-2210 line 21;
  RAB 2026-5's table row);
- the July to December 2026 and January to June 2027 table rows of RAB 2026-5
  and RAB 2026-10 (their daily factors are still checked through the RABs'
  sentences);
- days counted with the end date included (RAB 2026-5);
- the 3, 5, 8 and 12 month annualised periods and 22.5%, 45%, 67.5% and 90%
  (MI-2210 worksheet);
- rounding to whole dollars (MI-2210; RAB 2021-17's examples);
- an overpayment from the prior year applied to the first installment, and
  one in any quarter carried forward (MI-2210);
- interest to April 15 or the date paid, whichever is earlier (MI-2210);
- payments applied to the earliest underpayment first for interest (MI-2210;
  RAB 2021-17);
- a payment counting in the quarter it is received (MI-2210);
- the Line 24 instructions' 25% for nothing paid and 10% for paid short, as
  separate from the penalty sentence;
- a 12-month federal return under the not-required-to-file exception, and
  paying in full under the January 31 rule (the quotes stop before these
  words);
- a failure to pay as intentional disregard justifying 25% (RAB 2021-17);
- federal AGI as 2024 MI-1040 line 10;
- the written request for a penalty waiver (RAB 2025-15);
- "This relief is not automatic." (the March 19, 2026 notice);
- the text of the three worked examples: the MI-2210 example and RAB 2021-17
  Examples 6 and 7. The tests keep their inputs and answers.

Somebody with access should fetch the eight documents, check the quotes, and
replace `research-mi.md` with extracts.

## The worked examples are the strong check

The MI-2210 instructions work one example: $2,000 due each quarter, $1,000
short in April, $2,000 paid June 10, 56 days of interest and a $100 penalty in
the first quarter only. RAB 2021-17 works two more under the annualised
method, Examples 6 and 7, with the required installment for each quarter and
the penalty on each. Inputs and answers come from Treasury, and the tests
reproduce them in 2026 dates, with 2026 rates where the examples' interest
used 2020's. Example 7's fourth quarter is reproduced as the MI-2210 computes
it, not as the example does; `estimated_test.go` says why. Their text is not
checked in, as above.

## What is not here

- **The 2026 MI-2210.** Not published: the research found that the TY2026
  address returned 404. The 2025 form's arithmetic is used with the 2026 dates
  and rates.
- **The rate RAB the forms name.** The 2026 MI-1040ES and the 2025 MI-2210
  point to RAB 2025-13, which RAB 2026-5 and then RAB 2026-10 replaced.
- **The Internal Revenue Code.** MCL 206.301(11) adopts its computation, and
  the January 31 rule rests on 26 U.S.C. § 6654(h) as RAB 2021-17 note 29
  cites it. The section is not checked in here.
- **A Michigan holiday list.** None is checked in. Every 2026 due date the
  MI-1040ES prints is a weekday, so only weekends are applied, and the tests
  check the computed dates against the printed ones.
- **City income taxes.** Detroit's Form 5123 and the uniform city ordinance
  (MCL 141.662, 141.663) are out of scope.
